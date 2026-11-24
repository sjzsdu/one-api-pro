// realtime.go Qwen Omni 实时会话入口与固定渠道转发
// Proxies bounded manual Qwen WebSockets with prepaid durable response accounting.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/billing/ratio"
	handler "github.com/modelbus/one-api-pro/relay/handler"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	protocol "github.com/modelbus/one-api-pro/relay/session"
)

const realtimeProtocol = "qwen-omni-ws-manual-v1"
const realtimeFrameLimit = 512 << 10

type realtimePrice struct {
	Input       float64 `json:"input_price_per_million_tokens"`
	Output      float64 `json:"output_price_per_million_tokens"`
	PerRequest  float64 `json:"per_request_price"`
	Discount    float64 `json:"discount"`
	BillingType string  `json:"billing_type"`
}

func (p realtimePrice) quota(input, output int64) (int64, error) {
	return handler.MeteredQuota(&ratio.PriceResult{InputPrice: p.Input, OutputPrice: p.Output, PerRequestPrice: p.PerRequest, BillingType: p.BillingType}, p.Discount, []schema.UsageMetric{{Metric: "realtime.input_tokens", Quantity: input, Unit: "token"}, {Metric: "realtime.output_tokens", Quantity: output, Unit: "token"}})
}

type realtimeState struct {
	mu                      sync.Mutex
	row                     model.InferenceSession
	price                   realtimePrice
	planID                  int
	started                 int
	active                  bool
	uncertain               bool
	reservation             string
	inputBytes, outputBytes int64
	perTurnBound            int64
}

func realtimeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": gin.H{"type": "realtime_error", "message": message}})
}

func releaseUnusedRealtime(ctx context.Context, id, reason string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	status := "closed"
	if model.RefundInference(cleanup, id) != nil {
		status = "pending_reconciliation"
		reason = "refund_failed"
	}
	_ = model.DB.WithContext(cleanup).Model(&model.InferenceSession{}).Where("id = ?", id).Updates(map[string]any{"status": status, "close_reason": reason, "updated_at": time.Now().Unix()}).Error
}

func realtimeInt(c *gin.Context, name string, def, maxValue int) (int, error) {
	raw := c.Query(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxValue {
		return 0, fmt.Errorf("%s must be between 1 and %d", name, maxValue)
	}
	return n, nil
}

func realtimeUpstreamURL(base, modelName string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid realtime channel base URL")
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", errors.New("realtime channel requires an HTTP(S) base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api-ws/v1/realtime"
	u.RawPath = ""
	q := url.Values{}
	q.Set("model", modelName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// RelayRealtime serves the explicitly scoped Qwen manual WebSocket protocol after shared auth and routing.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayRealtime(c *gin.Context) {
	ctx := c.Request.Context()
	if c.GetInt(ctxkey.Id) <= 0 || c.GetInt(ctxkey.TokenId) <= 0 {
		realtimeError(c, 401, "API token authentication required")
		return
	}
	if !websocket.IsWebSocketUpgrade(c.Request) {
		realtimeError(c, 426, "WebSocket upgrade required")
		return
	}
	requested := c.GetString(ctxkey.RequestModel)
	actual := requested
	if mapped := c.GetStringMapString(ctxkey.ModelMapping)[actual]; mapped != "" {
		actual = mapped
	}
	if c.GetString(ctxkey.ChannelID) != "ali" || !protocol.SupportedModel(actual) {
		realtimeError(c, 400, "this endpoint implements Qwen 3.5 Omni manual WebSocket sessions on native Ali channels")
		return
	}
	capabilities, err := model.GetModelCapabilities("ali", actual)
	if err != nil {
		realtimeError(c, 500, "cannot load realtime capabilities")
		return
	}
	declared := false
	for _, mode := range capabilities.Operations["chat.generate"].Modes {
		declared = declared || mode == "realtime"
	}
	implemented := false
	for _, mode := range relay.RuntimeOperationModes("ali", actual)["chat.generate"] {
		implemented = implemented || mode == "realtime"
	}
	if !declared || !implemented {
		realtimeError(c, 400, "selected model does not expose realtime capability")
		return
	}
	turns, err := realtimeInt(c, "max_turns", 4, 16)
	if err != nil {
		realtimeError(c, 400, err.Error())
		return
	}
	outputLimit, err := realtimeInt(c, "max_output_tokens", 1024, 4096)
	if err != nil {
		realtimeError(c, 400, err.Error())
		return
	}
	seconds, err := realtimeInt(c, "max_seconds", 120, 120)
	if err != nil {
		realtimeError(c, 400, err.Error())
		return
	}
	priceResult, err := ratio.GetModelPrice(actual, requested)
	if err != nil {
		realtimeError(c, 422, "realtime model price is not configured")
		return
	}
	p := realtimePrice{Input: priceResult.InputPrice, Output: priceResult.OutputPrice, PerRequest: priceResult.PerRequestPrice, Discount: ratio.GetGroupDiscount(c.GetString(ctxkey.Group), actual, requested), BillingType: priceResult.BillingType}
	if p.Input < 0 || p.Output < 0 || p.PerRequest < 0 || p.Discount < 0 {
		realtimeError(c, 422, "invalid realtime price")
		return
	}
	bound, err := p.quota(protocol.MaxInputTokens, protocol.MaxOutputTokens)
	if err != nil {
		realtimeError(c, 422, err.Error())
		return
	}
	// max_tokens truncates the delivered result, not necessarily upstream work. Reserve the
	// protocol's conservative full-context/output bound before any paid response can start.
	if bound > 0 {
		bound++
	}
	if bound < 0 || bound > int64(^uint64(0)>>1)/int64(turns) {
		realtimeError(c, 422, "realtime reservation exceeds accounting limits")
		return
	}
	budget := bound * int64(turns)
	upstreamURL, err := realtimeUpstreamURL(c.GetString(ctxkey.BaseURL), actual)
	if err != nil {
		realtimeError(c, 400, err.Error())
		return
	}
	id := "rt_" + uuid.NewString()
	reservation := model.InferenceReservation{Key: id, UserID: c.GetInt(ctxkey.Id), TokenID: c.GetInt(ctxkey.TokenId), PlanID: c.GetInt(ctxkey.PlanId), ChannelID: c.GetInt(ctxkey.ChannelId), ModelName: requested, Quota: budget, BudgetOnly: true}
	if err = model.ReserveInference(ctx, reservation); err != nil {
		realtimeError(c, 403, "insufficient realtime prepaid quota")
		return
	}
	now := time.Now().Unix()
	snapshot, _ := json.Marshal(p)
	row := model.InferenceSession{ID: id, UserID: reservation.UserID, TokenID: reservation.TokenID, ChannelID: reservation.ChannelID, ModelName: requested, UpstreamModel: actual, Protocol: realtimeProtocol, Status: "active", PriceSnapshot: string(snapshot), BudgetQuota: budget, MaxTurns: turns, CreatedAt: now, UpdatedAt: now}
	if err = model.DB.WithContext(ctx).Create(&row).Error; err != nil {
		releaseUnusedRealtime(ctx, id, "session_creation_failed")
		realtimeError(c, 500, "cannot create realtime session")
		return
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	if config.RelayProxy != "" {
		proxyURL, proxyErr := url.Parse(config.RelayProxy)
		if proxyErr != nil {
			releaseUnusedRealtime(ctx, id, "invalid_relay_proxy")
			realtimeError(c, 500, "invalid relay proxy")
			return
		}
		dialer.Proxy = http.ProxyURL(proxyURL)
	}
	headers := http.Header{"Authorization": []string{c.Request.Header.Get("Authorization")}, "OpenAI-Beta": []string{"realtime=v1"}}
	upstream, resp, err := dialer.DialContext(ctx, upstreamURL, headers)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		releaseUnusedRealtime(ctx, id, "upstream_unavailable")
		realtimeError(c, 502, "realtime upstream connection failed")
		return
	}
	defer upstream.Close()
	initial, _ := json.Marshal(map[string]any{"type": "session.update", "session": map[string]any{"model": actual, "turn_detection": nil, "max_tokens": outputLimit, "voice": "Tina", "modalities": []string{"text", "audio"}}})
	_ = upstream.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err = upstream.WriteMessage(websocket.TextMessage, initial); err != nil {
		releaseUnusedRealtime(ctx, id, "initialization_failed")
		realtimeError(c, 502, "realtime initialization failed")
		return
	}
	responseHeaders := http.Header{"X-OneAPI-Realtime-Protocol": []string{realtimeProtocol}, "X-OneAPI-Realtime-Session": []string{id}, "X-OneAPI-Realtime-Budget-Quota": []string{strconv.FormatInt(budget, 10)}}
	client, err := (&websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}).Upgrade(c.Writer, c.Request, responseHeaders)
	if err != nil {
		releaseUnusedRealtime(ctx, id, "client_upgrade_failed")
		return
	}
	defer client.Close()
	s := &realtimeState{row: row, price: p, planID: reservation.PlanID, perTurnBound: bound}
	s.run(c, client, upstream, outputLimit, time.Duration(seconds)*time.Second)
}

func (s *realtimeState) startTurn(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active || s.started >= s.row.MaxTurns || s.perTurnBound > s.row.BudgetQuota-s.row.TotalQuota {
		return errors.New("realtime response or prepaid turn limit reached")
	}
	key := fmt.Sprintf("%s:turn:%d", s.row.ID, s.started+1)
	r := model.InferenceReservation{Key: key, UserID: s.row.UserID, TokenID: s.row.TokenID, PlanID: s.planID, ChannelID: s.row.ChannelID, ModelName: s.row.ModelName, Quota: 0}
	if err := model.ReserveInference(ctx, r); err != nil {
		return errors.New("realtime plan request quota exhausted")
	}
	result := model.DB.WithContext(ctx).Model(&model.InferenceSession{}).Where("id = ? AND status = ?", s.row.ID, "active").Updates(map[string]any{"pending_response": true, "pending_reservation": key, "updated_at": time.Now().Unix()})
	if result.Error != nil || result.RowsAffected != 1 {
		_ = model.RefundInference(context.WithoutCancel(ctx), key)
		return errors.New("cannot record realtime response intent")
	}
	s.active = true
	s.started++
	s.reservation = key
	return nil
}

func (s *realtimeState) run(c *gin.Context, client, upstream *websocket.Conn, maxTokens int, duration time.Duration) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), duration)
	defer cancel()
	client.SetReadLimit(realtimeFrameLimit)
	upstream.SetReadLimit(realtimeFrameLimit)
	deadline := time.Now().Add(duration)
	_ = client.SetReadDeadline(deadline)
	_ = upstream.SetReadDeadline(deadline)
	clientDone := make(chan struct{})
	failure := make(chan string, 1)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
			upstream.Close()
		case <-clientDone:
		}
	}()
	go func() {
		defer close(clientDone)
		for {
			kind, data, err := client.ReadMessage()
			if err != nil {
				select {
				case failure <- "client_closed":
				default:
				}
				upstream.Close()
				return
			}
			fail := ""
			event := ""
			if kind != websocket.TextMessage {
				fail = "text_json_frames_required"
			} else {
				event, err = protocol.ValidateClientEvent(data, s.row.UpstreamModel, maxTokens)
				if err != nil {
					fail = "invalid_session_event"
				}
			}
			s.mu.Lock()
			s.inputBytes += int64(len(data))
			if s.inputBytes > 8<<20 {
				fail = "input_byte_budget_exhausted"
			}
			s.mu.Unlock()
			if fail == "" && event == "response.create" {
				if err = s.startTurn(ctx); err != nil {
					fail = "response_budget_exhausted"
				}
			}
			if fail != "" {
				select {
				case failure <- fail:
				default:
				}
				upstream.Close()
				return
			}
			_ = upstream.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err = upstream.WriteMessage(kind, data); err != nil {
				upstream.Close()
				return
			}
		}
	}()
	reason := "upstream_disconnected"
	for {
		kind, data, err := upstream.ReadMessage()
		if err != nil {
			select {
			case reason = <-failure:
			default:
			}
			if ctx.Err() != nil {
				reason = "session_expired"
			}
			break
		}
		if kind != websocket.TextMessage {
			reason = "invalid_upstream_frame"
			s.mu.Lock()
			s.uncertain = true
			s.mu.Unlock()
			break
		}
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &event) != nil {
			reason = "invalid_upstream_event"
			s.mu.Lock()
			s.uncertain = true
			s.mu.Unlock()
			break
		}
		s.mu.Lock()
		s.outputBytes += int64(len(data))
		oversized := s.outputBytes > 64<<20
		s.mu.Unlock()
		if oversized {
			reason = "output_byte_budget_exhausted"
			break
		}
		if event.Type == "response.created" {
			s.mu.Lock()
			if !s.active {
				s.uncertain = true
				reason = "unsolicited_response"
			}
			s.mu.Unlock()
			if reason == "unsolicited_response" {
				break
			}
		}
		if event.Type == "response.done" {
			completion, parseErr := protocol.ParseCompletion(data)
			if parseErr != nil {
				reason = "missing_or_invalid_usage"
				s.mu.Lock()
				s.uncertain = true
				s.mu.Unlock()
				break
			}
			quota, costErr := s.price.quota(completion.InputTokens, completion.OutputTokens)
			if costErr != nil {
				reason = "invalid_usage_price"
				s.mu.Lock()
				s.uncertain = true
				s.mu.Unlock()
				break
			}
			added, recordErr := model.RecordInferenceSessionTurn(ctx, s.row.ID, completion.ID, completion.InputTokens, completion.OutputTokens, quota, completion.UsageJSON)
			if recordErr != nil {
				reason = "usage_conflict_or_budget_exhausted"
				s.mu.Lock()
				s.uncertain = true
				s.mu.Unlock()
				break
			}
			if added {
				s.mu.Lock()
				turnKey := s.reservation
				s.active = false
				s.reservation = ""
				s.row.TotalQuota += quota
				s.row.TurnCount++
				s.mu.Unlock()
				if turnKey == "" || model.SettleInference(ctx, turnKey, 0, completion.UsageJSON) != nil {
					reason = "turn_settlement_failed"
					s.mu.Lock()
					s.uncertain = true
					s.mu.Unlock()
					break
				}
			}
		}
		_ = client.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err = client.WriteMessage(kind, data); err != nil {
			reason = "client_closed"
			break
		}
		if event.Type == "error" {
			reason = "upstream_error"
			break
		}
	}
	if reason != "client_closed" {
		event, _ := json.Marshal(gin.H{"type": "error", "error": gin.H{"type": "gateway_realtime_error", "code": reason, "message": "session stopped; inspect its accounting status before retrying"}})
		_ = client.SetWriteDeadline(time.Now().Add(time.Second))
		_ = client.WriteMessage(websocket.TextMessage, event)
	}
	client.Close()
	upstream.Close()
	<-clientDone
	s.mu.Lock()
	uncertain := s.uncertain || s.active
	s.mu.Unlock()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer finishCancel()
	if err := model.FinishInferenceSession(finishCtx, s.row.ID, uncertain, reason); err != nil {
		logger.Errorf(finishCtx, "realtime session %s requires accounting reconciliation", s.row.ID)
	}
}

// GetRealtimeSession returns only the authenticated owner's accounting and lifecycle state.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetRealtimeSession(c *gin.Context) {
	var s model.InferenceSession
	if err := model.DB.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", c.Param("id"), c.GetInt(ctxkey.Id)).First(&s).Error; err != nil {
		realtimeError(c, 404, "realtime session not found")
		return
	}
	c.JSON(200, s)
}
