// model_wildcard_test.go verifies that "/v1/models" expands a token's "*"
// whitelist into the concrete models of the user's group, instead of returning
// the literal "*" that makes OpenAI-compatible clients reject real models.
package controller

import (
	"bytes"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
)

func TestTokenModelsHaveWildcard(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  bool
	}{
		{name: "literal wildcard", input: []string{"*"}, want: true},
		{name: "wildcard with spaces", input: []string{"chat", " * ", "gpt-4"}, want: true},
		{name: "exact only", input: []string{"chat", "qwen-plus"}, want: false},
		{name: "empty", input: nil, want: false},
		{name: "blank", input: []string{""}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenModelsHaveWildcard(tc.input); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExpandWildcardTokenModels(t *testing.T) {
	cases := []struct {
		name        string
		tokenModels []string
		groupModels []string
		want        []string
	}{
		{
			name:        "wildcard replaced by group models",
			tokenModels: []string{"*"},
			groupModels: []string{"deepseek", "qwen-plus"},
			want:        []string{"deepseek", "qwen-plus"},
		},
		{
			name:        "explicit models preserved alongside expansion",
			tokenModels: []string{"chat", "*"},
			groupModels: []string{"deepseek", "qwen-plus"},
			want:        []string{"chat", "deepseek", "qwen-plus"},
		},
		{
			name:        "duplicates and blanks removed",
			tokenModels: []string{"*", " qwen-plus", " ", "chat", "qwen-plus"},
			groupModels: []string{"qwen-plus", "deepseek"},
			want:        []string{"qwen-plus", "chat", "deepseek"},
		},
		{
			name:        "exact whitelist unchanged",
			tokenModels: []string{"chat", "gpt-4"},
			groupModels: []string{"deepseek", "qwen-plus"},
			want:        []string{"chat", "gpt-4"},
		},
		{
			name:        "wildcard with empty group yields explicit only",
			tokenModels: []string{"chat", "*"},
			groupModels: nil,
			want:        []string{"chat"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandWildcardTokenModels(tc.tokenModels, tc.groupModels)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestListModelsExpandsWildcard is the end-to-end regression: a token whose
// whitelist is "*,chat" must list the group's concrete models (qwen-plus) and
// never expose the literal "*".
func TestListModelsExpandsWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:wildcard-models?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	defer func() {
		model.DB, common.RedisEnabled = oldDB, oldRedis
		sqlDB.Close()
	}()
	db.Create(&model.User{Id: 701, Username: "wildcard", Status: 1, Group: "default", AccessToken: "wcacc", AffCode: "wcaff"})
	db.Create(&model.Token{Id: 701, UserId: 701, Key: "wildcardkey", Status: 1, Models: stringPtr("*,chat"), UnlimitedQuota: true, ExpiredTime: -1})
	db.Create(&model.Channel{Status: 1, Group: "default", Models: "qwen-plus,deepseek"})

	r := gin.New()
	r.GET("/v1/models", middleware.TokenAuth(), ListModels)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-wildcardkey")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	for _, want := range []string{`"id":"qwen-plus"`, `"id":"deepseek"`, `"id":"chat"`} {
		if !bytes.Contains(w.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"id":"*"`)) {
		t.Fatalf("literal wildcard leaked in %s", body)
	}
}

func stringPtr(s string) *string { return &s }
