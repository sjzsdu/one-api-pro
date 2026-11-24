// audio.go 火山豆包语音合成（seed-tts-2.0）V3 原生协议转换
// Converts OpenAI speech requests to Volcengine V3 unidirectional NDJSON TTS.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: opencode
package doubao

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/random"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// 火山 V3 语音合成 NDJSON 响应码：0 为音频块（可能夹带 data=null 的元数据行），
// 20000000 为合成结束；其余按上游错误处理。
const (
	volcTTSSuccess = 0
	volcTTSDone    = 20000000
)

// seedTTSFormat 把 OpenAI response_format 归一到火山 audio_params.format。
// 火山支持 wav / pcm / ogg_opus / mp3；ogg 映射到 ogg_opus，其余原样交由上游校验。
func seedTTSFormat(requestFormat string) string {
	format := strings.ToLower(strings.TrimSpace(requestFormat))
	if format == "" {
		format = "wav"
	}
	if format == "ogg" {
		return "ogg_opus"
	}
	return format
}

// seedTTSSpeechRate 把 OpenAI speed（0.5–2.0）映射到火山 speech_rate（[-50,100]）。
// 1.0 → 0，1.07 → 7，2.0 → 100。
func seedTTSSpeechRate(speed *float64) int {
	if speed == nil {
		return 0
	}
	if math.IsNaN(*speed) || math.IsInf(*speed, 0) {
		return 0
	}
	rate := int(math.Round((*speed - 1) * 100))
	if rate < -50 {
		return -50
	}
	if rate > 100 {
		return 100
	}
	return rate
}

// volcTTSAudioParams 是火山 req_params.audio_params。
type volcTTSAudioParams struct {
	Format       string `json:"format"`
	SampleRate   int    `json:"sample_rate"`
	SpeechRate   int    `json:"speech_rate"`
	LoudnessRate int    `json:"loudness_rate"`
}

// volcTTSReqParams 是火山 V3 请求的 req_params 主体。
type volcTTSReqParams struct {
	Text        string             `json:"text"`
	Speaker     string             `json:"speaker"`
	AudioParams volcTTSAudioParams `json:"audio_params"`
}

// volcTTSRequest 是火山 V3 单向流式语音合成请求体。
type volcTTSRequest struct {
	User      map[string]string `json:"user"`
	ReqParams volcTTSReqParams  `json:"req_params"`
}

// ConvertAudioSpeechRequest 实现 adaptor.AudioSpeechRequester。
// OpenAI 扁平字段转换为火山 V3 req_params；voice 必填且直接作为 speaker 音色 ID。
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) ConvertAudioSpeechRequest(request *schema.AudioSpeechRequest) (any, error) {
	if request == nil || strings.TrimSpace(request.Input) == "" {
		return nil, errors.New("input is required")
	}
	if strings.TrimSpace(request.Voice) == "" {
		return nil, errors.New("voice is required: use a Volcengine speaker id, e.g. zh_female_vv_uranus_bigtts")
	}
	return &volcTTSRequest{
		User: map[string]string{"uid": "one-api-pro"},
		ReqParams: volcTTSReqParams{
			Text:    request.Input,
			Speaker: request.Voice,
			AudioParams: volcTTSAudioParams{
				Format:       seedTTSFormat(request.ResponseFormat),
				SampleRate:   24000,
				SpeechRate:   seedTTSSpeechRate(request.Speed),
				LoudnessRate: 0,
			},
		},
	}, nil
}

// volcTTSLine 是火山 V3 NDJSON 响应的单行结构。
type volcTTSLine struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
	Header  struct {
		Message string `json:"message"`
	} `json:"header"`
}

// AudioSpeechResponse 实现 adaptor.AudioSpeechHandler。
// 逐行解析 NDJSON，累积 base64 音频块；结束后回传二进制音频与字符用量。
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) AudioSpeechResponse(c *gin.Context, resp *http.Response, m *meta.Meta) (*schema.AudioSpeechResponse, *schema.ErrorWithStatusCode) {
	defer resp.Body.Close()
	// m 保留在签名中以匹配 adaptor.AudioSpeechHandler 接口；火山 V3 的响应
	// 不依赖渠道元数据，实际音频格式由请求 audio_params.format 决定。
	_ = m

	var audio []byte
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
readLines:
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item volcTTSLine
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, openai.ErrorWrapper(err, "volc_tts_malformed_line", http.StatusBadGateway)
		}
		switch item.Code {
		case volcTTSSuccess:
			if item.Data != "" {
				chunk, err := base64.StdEncoding.DecodeString(item.Data)
				if err != nil {
					return nil, openai.ErrorWrapper(err, "volc_tts_bad_audio_chunk", http.StatusBadGateway)
				}
				audio = append(audio, chunk...)
			}
		case volcTTSDone:
			// 合成结束，忽略后续行。
			break readLines
		default:
			message := item.Message
			if message == "" {
				message = item.Header.Message
			}
			if message == "" {
				message = fmt.Sprintf("upstream code %d", item.Code)
			}
			return nil, openai.ErrorWrapper(errors.New(message), "volc_tts_upstream_error", http.StatusBadGateway)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, openai.ErrorWrapper(err, "volc_tts_read_failed", http.StatusBadGateway)
	}

	if len(audio) == 0 {
		return nil, openai.ErrorWrapper(errors.New("Volcengine TTS produced no audio"), "volc_tts_audio_empty", http.StatusBadGateway)
	}
	// 火山 V3 返回的是音频字节本身（格式由 audio_params.format 指定），
	// 用通用二进制类型避免声言错误的具体编码。
	return &schema.AudioSpeechResponse{
		Body:        audio,
		ContentType: "application/octet-stream",
	}, nil
}

// NewVolcTTSRequestID 为每次合成生成唯一 X-Api-Request-Id。
// 版本: v0.0.28
// 日期: 2026-11-23
func NewVolcTTSRequestID() string {
	return random.GetUUID()
}
