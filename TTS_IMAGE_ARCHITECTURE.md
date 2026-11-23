<!--
网关 TTS 与文生图架构及配置说明 / Gateway speech and image architecture.
版本: v0.0.28
日期: 2026-11-23
作者: Codex
-->
# 网关 TTS + 文生图能力架构

网关对外提供 OpenAI 风格的 `/v1/audio/speech` 和 `/v1/images/generations`；百炼原生协议由 `ali`（渠道类型 17）适配器转换。OpenAI Compatible 渠道继续原样转发请求体，转写和翻译保持原有路径。OpenAI Compatible 的协议透传保持原样，但 TTS 计费修正为 Unicode 字符数、输入单价和分组折扣，不能将这项计费修正理解为全部行为不变。

## 能力边界

必选 `Adaptor` 接口保持不变，媒体功能通过可选接口接入：

```go
type AudioSpeechRequester interface {
    ConvertAudioSpeechRequest(*model.AudioSpeechRequest) (any, error)
}
type AudioSpeechHandler interface {
    AudioSpeechResponse(*gin.Context, *http.Response, *meta.Meta) (*model.AudioSpeechResponse, *model.ErrorWithStatusCode)
}
type ImageRequestRewriter interface {
    RewriteImageRequest(*model.ImageRequest) (any, error)
}
```

`AudioSpeechResponse` 返回音频字节、Content-Type、字符数或输入/输出 token 用量，不直接提交 HTTP 响应。handler 在解析、下载和写出成功后结算；失败同步退回预扣并失效余额缓存，订阅失败不累计套餐额度或请求次数。图片响应转换失败同样不结算。

## 百炼 TTS

请求地址为 `{base_url}/api/v1/services/audio/tts/SpeechSynthesizer`。转换示例：

```json
{
  "model": "cosyvoice-v3-flash",
  "input": {
    "text": "你好",
    "voice": "longanhuan_v3",
    "format": "wav",
    "sample_rate": 24000,
    "rate": 1.1
  }
}
```

- 支持 `wav`、`mp3`、`pcm`、`opus`，省略格式时默认 `wav`。`flac`、`ogg`、`aac` 等未实现转码，返回 400，不静默更换编码。
- `speed` 精确映射到 `input.rate`，支持 `[0.5, 2.0]`；省略时使用上游默认语速，显式 0 或越界返回 400。
- `model`、`input`、`voice` 必填；输入最多 4096 个 Unicode 字符。音色 ID 必须与模型匹配，具体以官方音色表为准。
- 非流式响应中的 `output.audio.url` 由网关下载，客户端收到二进制音频。

协议参考：[CosyVoice HTTP](https://help.aliyun.com/en/model-studio/cosyvoice-tts-http-api)、[Qwen-Audio HTTP](https://help.aliyun.com/zh/model-studio/qwen-audio-tts-http-api)。

## 计费口径

默认价是网关售价，可由管理员覆盖，并非承诺与上游成本相同。`BillingType=per_request` 继续按次价结算。

- 字符型 TTS：配置 `InputPrice` 为每百万 Unicode 字符的人民币价格。使用上游 `usage.characters`（缺失时使用输入 rune 数），乘分组折扣；不平均输入/输出单价，也不使用 UTF-8 字节长度。`OutputPrice` 对字符模型不参与费用计算。
- `qwen-audio-3.1-tts-flash`：`InputPrice`/`OutputPrice` 是每百万输入/输出 token 的价格，使用上游实际 `input_tokens` 和 `output_tokens` 结算。缺失、负数或全零用量返回 502 并退款，避免未知用量按免费成功处理。
- 原生 TTS 的非零售价请求，按输入 token 估算加现有额外预留额度预扣，最终按响应的真实单位调整；这不是最终精确费用。字符模型也使用该原生预留路径。
- 零价保持免费；正费用不足一个微元时取一个微元。余额模式的消费日志标明字符或 token 单位；订阅仍累计套餐虚拟余额和窗口请求次数。
- 默认模型价只初始化新库；存量库可在定价页配置精确模型价，未配置时使用已有 `*` 兜底价。

## Wan2.7 文生图

`wan2.7-image` 和 `wan2.7-image-pro` 使用同步接口：

- URL：`{base_url}/api/v1/services/aigc/multimodal-generation/generation`。
- 请求：`input.messages=[{"role":"user","content":[{"text":"提示词"}]}]`。
- 参数：尺寸 `1080x1440` 转成 `1080*1440`，`n` 为 1–4，`seed` 范围 `[0,2147483647]`；尺寸、模型地域和实际可用规格仍需满足上游限制。
- 不发送异步头、不轮询任务；从 `output.choices[].message.content[].image` 读取结果。
- `response_format=url`（默认）返回地址；`b64_json` 安全下载后编码。无图片、解析失败或下载失败返回错误，不产生成功消费。
- Wan2.7 不支持 `negative_prompt`，该字段不发送给上游；需要排除的内容请在正向提示词中描述。

旧版万相模型继续现有 `text2image/image-synthesis` 异步协议。本次没有将旧协议替换为 Wan2.7，也没有扩展旧模型的轮询实现。

协议参考：[Wan2.7 图像生成与编辑](https://help.aliyun.com/zh/model-studio/wan-image-generation-and-editing-api-reference)。

## 媒体下载安全

TTS 音频和 Wan2.7 的 Base64 图片使用同一个独立下载器：

- 请求与重定向不带渠道 API Key、Authorization、Cookie 或 Referer。
- 仅允许 HTTP/HTTPS，禁止 URL 用户凭证，拒绝私有、loopback、link-local 和保留 IP；DNS 在拨号时解析并固定连接到已验证的公有 IP，避免检查后重新解析带来的 DNS 重绑定。
- 每次重定向重新校验目标，最多 5 跳。总超时 60 秒，每个文件最多 32 MiB；错误不记录签名 URL 参数。
- 下载器直接访问公有媒体地址，**不使用 RELAY_PROXY**。部署环境需要允许公有媒体域名出站；API 请求仍沿用已有代理配置。

## 配置与验收

1. 新建类型 17「阿里通义千问」渠道，配置对应地域百炼 API Key。优先使用官方业务空间端点，例如 `https://<WorkspaceId>.cn-beijing.maas.aliyuncs.com`；API Key 与端点地域必须一致。
2. 按需启用 `cosyvoice-v3-flash`、`qwen-audio-3.1-tts-flash`、`wan2.7-image`，给令牌追加相应模型权限，并设置网关售价。
3. 类型 49 的 compatible-mode 渠道不是本次原生媒体能力入口；现有聊天渠道可继续使用。

```bash
# 将 GATEWAY_BASE_URL 与 GATEWAY_TOKEN 设置为实际网关与令牌。
curl --fail-with-body "$GATEWAY_BASE_URL/v1/audio/speech" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"cosyvoice-v3-flash","input":"你好","voice":"longanhuan_v3","response_format":"wav","speed":1.1}' \
  -o /tmp/gateway-tts.wav

curl --fail-with-body "$GATEWAY_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-image","prompt":"山间日出","n":1,"size":"1080x1440","response_format":"url"}'
```

自动回归覆盖请求序列化、同步图片结果、二进制响应、格式与语速校验、下载凭证隔离和私网拒绝、失败退款、失败订阅不耗用、实际用量和零价。使用假上游验证协议，不代表已用生产 API Key 完成真实模型调用。

## 火山 TTS 状态

**尚未实现火山原生 TTS 适配器。** 将 OpenAI Compatible 的 base_url 指向 `openspeech.bytedance.com/api/v1` 不能接通：网关会请求 `/api/v1/audio/speech`，而火山 V1 是 `/api/v1/tts`，请求结构、鉴权和 Base64 JSON 响应也不同。需要另行实现原生适配器，或接入真正提供 OpenAI speech 协议的中间服务。

参考：[火山官方 V1 HTTP TTS](https://docs.volcengine.com/docs/doubao-voice/http-one-timecompositenon-streaming-interface-v1-not-recommended?lang=en)。
