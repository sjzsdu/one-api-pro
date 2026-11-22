// alias_pool_prefix_test.go 结构化消息前缀身份与短公共内容保护
// Verifies canonical histories without retaining prompt content in routing indexes.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAliasPrefix_CanonicalAndAppend verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_CanonicalAndAppend(t *testing.T) {
	first := `{"messages":[{"role":"system","content":"stable"},{"role":"user","content":"Please explain the architecture and tradeoffs of this application."}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"temperature":0}`
	reordered := `{ "temperature":1,"tools":[{"function":{"parameters":{"type":"object"},"name":"lookup"},"type":"function"}],"messages":[{"content":"stable","role":"system"},{"content":"Please explain the architecture and tradeoffs of this application.","role":"user"}],"stream":true}`
	a := buildAliasPrefixIdentity("/v1/chat/completions", []byte(first))
	b := buildAliasPrefixIdentity("/v1/chat/completions", []byte(reordered))
	if a.Current == "" || a.Current != b.Current {
		t.Fatalf("canonical mismatch: %+v %+v", a, b)
	}
	next := strings.Replace(first, `application."}]`, `application."},{"role":"assistant","content":"An explanation"},{"role":"user","content":"Please continue with more implementation details."}]`, 1)
	c := buildAliasPrefixIdentity("/v1/chat/completions", []byte(next))
	if c.Current == a.Current || len(c.Hashes) < 3 || c.Hashes[len(c.Hashes)-1] != a.Current {
		t.Fatalf("append did not preserve prior boundary: %+v", c)
	}
	for _, changed := range []string{strings.Replace(first, "stable", "different", 1), strings.Replace(first, "lookup", "different", 1), strings.Replace(first, "object", "string", 1)} {
		v := buildAliasPrefixIdentity("/v1/chat/completions", []byte(changed))
		for _, h := range v.Hashes {
			if h == a.Current {
				t.Fatal("changed prompt context reused prefix")
			}
		}
	}
	short := `{"messages":[{"role":"system","content":"` + strings.Repeat("system", 200) + `"},{"role":"user","content":"你好"}]}`
	if v := buildAliasPrefixIdentity("/v1/chat/completions", []byte(short)); v.Current != "" || len(v.Hashes) != 0 {
		t.Fatal("system inflated short user content")
	}
}

// TestAliasPrefix_PreservesStructuredResponses verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_PreservesStructuredResponses(t *testing.T) {
	body := `{"instructions":"stable","input":[{"role":"user","content":[{"type":"input_text","text":"Please explain the architecture and tradeoffs of this application."},{"type":"input_image","image_url":"image-a"}]},{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{\"a\":1}"}]}`
	a := buildAliasPrefixIdentity("/v1/responses", []byte(body))
	if a.Current == "" {
		t.Fatal("responses history not recognized")
	}
	for _, changed := range []string{strings.Replace(body, "image-a", "image-b", 1), strings.Replace(body, `1}`, `2}`, 1), strings.Replace(body, "stable", "changed", 1)} {
		b := buildAliasPrefixIdentity("/v1/responses", []byte(changed))
		if b.Current == a.Current {
			t.Fatal("response media/tool/instructions lost")
		}
	}
	if v := buildAliasPrefixIdentity("/v1/completions", []byte(`{"prompt":"a long prompt without structured conversation history"}`)); v.Current != "" {
		t.Fatal("unstructured prompt inferred")
	}
}

// TestAliasPrefix_AutomaticSession verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_AutomaticSession(t *testing.T) {
	if e := model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UserModelAlias{}); e != nil {
		t.Fatal(e)
	}
	id, _ := seedUser(t, "prefix-auto", model.RoleCommonUser, "prefixautokey", true)
	channel := model.Channel{Status: 1, Type: 1, Key: "fake", Models: "prefix-a,prefix-b", Group: "default"}
	if e := channel.Insert(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{})
		model.DB.Delete(&channel)
	}()
	aliases := []model.UserModelAlias{{Alias: "auto-prefix", Targets: []model.UserModelAliasTarget{{Model: "prefix-a", Weight: 1}, {Model: "prefix-b", Weight: 1}}}}
	if e := model.SaveUserModelAliases(context.Background(), id, aliases); e != nil {
		t.Fatal(e)
	}
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		state := GetAliasPool(c)
		CompleteAliasPool(c)
		c.JSON(200, gin.H{"key": state.SessionKey, "model": c.GetString(ctxkey.RequestModel)})
	})
	call := func(body, session string) map[string]string {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-prefixautokey")
		if session != "" {
			req.Header.Set("X-Session-ID", session)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		result := map[string]string{}
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	first := `{"model":"auto-prefix","messages":[{"role":"user","content":"Please explain the architecture and tradeoffs of this application."}]}`
	identity := buildAliasPrefixIdentity("/v1/chat/completions", []byte(first))
	aliases[0].UserId = id
	scope := model.AliasPrefixScope(id, aliases[0], identity.Protocol)
	explicit := call(first, "explicit-priority")
	if got, e := model.FindAliasPrefixSession(scope, identity.Hashes); e != nil || got != "" {
		t.Fatal("explicit session polluted automatic index")
	}
	a := call(first, "")
	if a["key"] == "" || a["key"] == explicit["key"] {
		t.Fatal("automatic session missing or collides with explicit ID")
	}
	next := strings.Replace(first, `application."}]`, `application."},{"role":"assistant","content":"An explanation"},{"role":"user","content":"Continue with details."}]`, 1)
	b := call(next, "")
	if b["key"] != a["key"] || b["model"] != a["model"] {
		t.Fatalf("history not sticky: %+v %+v", a, b)
	}
	short := call(`{"model":"auto-prefix","messages":[{"role":"user","content":"你好"}]}`, "")
	if short["key"] != "" {
		t.Fatal("short shared content created a deterministic session")
	}
}

// TestAliasPrefix_StructuredBoundariesAndLimit verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_StructuredBoundariesAndLimit(t *testing.T) {
	raw := `{"messages":[{"role":"user","content":[{"type":"text","text":"Please explain the architecture and tradeoffs of this application."},{"type":"image_url","image_url":{"url":"image-a"}}]},{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"a\":1}"}}]}],"response_format":{"type":"json_object"}}`
	a := buildAliasPrefixIdentity("/v1/chat/completions", []byte(raw))
	for _, body := range []string{strings.Replace(raw, "image-a", "image-b", 1), strings.Replace(raw, "call-1", "call-2", 1), strings.Replace(raw, `1}`, `2}`, 1), strings.Replace(raw, "json_object", "text", 1)} {
		if b := buildAliasPrefixIdentity("/v1/chat/completions", []byte(body)); b.Current == a.Current {
			t.Fatal("structured history or root format ignored")
		}
	}
	var root map[string]any
	json.Unmarshal([]byte(raw), &root)
	messages := root["messages"].([]any)
	for i := 0; i < 150; i++ {
		messages = append(messages, map[string]any{"role": "assistant", "content": fmt.Sprint(i)})
	}
	root["messages"] = messages
	body, _ := json.Marshal(root)
	if b := buildAliasPrefixIdentity("/v1/chat/completions", body); len(b.Hashes) != 128 || b.Hashes[0] != b.Current {
		t.Fatal("history lookup is not bounded or longest first")
	}
}

// TestAliasPrefix_ConcurrentFirstRequests verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_ConcurrentFirstRequests(t *testing.T) {
	id, _ := seedUser(t, "prefix-concurrent", model.RoleCommonUser, "prefixconcurrent", true)
	if e := model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}); e != nil {
		t.Fatal(e)
	}
	ch := model.Channel{Status: 1, Type: 1, Key: "fake", Models: "concurrent-a,concurrent-b", Group: "default"}
	ch.Insert()
	defer func() { model.DB.Where("channel_id = ?", ch.Id).Delete(&model.Ability{}); model.DB.Delete(&ch) }()
	items := []model.UserModelAlias{{Alias: "prefix-concurrent", Targets: []model.UserModelAliasTarget{{Model: "concurrent-a", Weight: 1}, {Model: "concurrent-b", Weight: 1}}}}
	if e := model.SaveUserModelAliases(context.Background(), id, items); e != nil {
		t.Fatal(e)
	}
	// Prime DB/cache reads before concurrent requests against SQLite's single in-memory connection.
	model.GetUserModelAliasesWithGroupModels(context.Background(), id)
	body := `{"model":"prefix-concurrent","messages":[{"role":"user","content":"Please explain the architecture and tradeoffs of this application."}]}`
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		state := GetAliasPool(c)
		c.JSON(200, gin.H{"model": state.Targets[0].Model, "key": state.SessionKey})
	})
	// TokenAuth's DB work is serialized here; model-level Claim remains concurrently exercised below.
	identity := buildAliasPrefixIdentity("/v1/chat/completions", []byte(body))
	items[0].UserId = id
	key := model.AliasPrefixSessionKey(model.AliasPrefixScope(id, items[0], identity.Protocol), identity.Current)
	var wg sync.WaitGroup
	got := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "concurrent-a"
			if i%2 == 0 {
				name = "concurrent-b"
			}
			binding, e := model.ClaimAliasSession(key, model.AliasSessionBinding{Model: name}, time.Minute)
			if e != nil {
				got <- "ERROR"
			} else {
				got <- binding.Model
			}
		}(i)
	}
	wg.Wait()
	close(got)
	winner := ""
	for name := range got {
		if winner == "" {
			winner = name
		}
		if name != winner {
			t.Fatal("automatic first-request binding split")
		}
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-prefixconcurrent")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), winner) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
