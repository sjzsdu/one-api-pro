// option_env_test.go 验证注册开关环境变量优先于数据库 option
// Verifies env-managed registration switches take precedence over DB options.
// 版本: v0.0.28
// 日期: 2026-11-14
// 作者: opencode
package model

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common/config"
	"gorm.io/gorm"
)

// setupOptionEnvTestDB 用内存 SQLite 准备 options 表。
// 版本: v0.0.28
// 日期: 2026-11-14
func setupOptionEnvTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Option{}); err != nil {
		t.Fatalf("migrate Option: %v", err)
	}
	DB = db
}

// assertRegisterSwitches 断言运行时与 OptionMap 里的注册开关值。
// 版本: v0.0.28
// 日期: 2026-11-14
func assertRegisterSwitches(t *testing.T, want bool) {
	t.Helper()
	wantString := "false"
	if want {
		wantString = "true"
	}
	if config.RegisterEnabled != want {
		t.Errorf("config.RegisterEnabled = %v, want %v", config.RegisterEnabled, want)
	}
	if config.PasswordRegisterEnabled != want {
		t.Errorf("config.PasswordRegisterEnabled = %v, want %v", config.PasswordRegisterEnabled, want)
	}
	config.OptionMapRWMutex.RLock()
	defer config.OptionMapRWMutex.RUnlock()
	if got := config.OptionMap["RegisterEnabled"]; got != wantString {
		t.Errorf("OptionMap[RegisterEnabled] = %q, want %q", got, wantString)
	}
	if got := config.OptionMap["PasswordRegisterEnabled"]; got != wantString {
		t.Errorf("OptionMap[PasswordRegisterEnabled] = %q, want %q", got, wantString)
	}
}

// TestRegisterEnabledEnvOverridesDatabaseOption 模拟公网部署：
// env 关闭注册，但 DB 里残留 true，且后台尝试重新打开。
// 版本: v0.0.28
// 日期: 2026-11-14
func TestRegisterEnabledEnvOverridesDatabaseOption(t *testing.T) {
	t.Setenv("REGISTER_ENABLED", "false")
	t.Setenv("PASSWORD_REGISTER_ENABLED", "false")
	t.Cleanup(func() {
		_ = os.Unsetenv("REGISTER_ENABLED")
		_ = os.Unsetenv("PASSWORD_REGISTER_ENABLED")
		config.ReloadConfig()
	})
	config.ReloadConfig()
	setupOptionEnvTestDB(t)

	// 数据库里残留「允许注册」的旧配置。
	for _, key := range []string{"RegisterEnabled", "PasswordRegisterEnabled"} {
		if err := DB.Create(&Option{Key: key, Value: "true"}).Error; err != nil {
			t.Fatalf("seed DB option %s: %v", key, err)
		}
	}

	// 1) 首次 InitOptionMap() 不能被 DB 覆盖。
	InitOptionMap()
	assertRegisterSwitches(t, false)

	// 2) SyncOptions() 的等价路径（周期性重新加载）也不能覆盖。
	loadOptionsFromDatabase()
	assertRegisterSwitches(t, false)

	// 3) 后台设置页通过 UpdateOption() 写入 DB 后，运行时仍然以 env 为准。
	if err := UpdateOption("RegisterEnabled", "true"); err != nil {
		t.Fatalf("UpdateOption: %v", err)
	}
	if err := UpdateOption("PasswordRegisterEnabled", "true"); err != nil {
		t.Fatalf("UpdateOption: %v", err)
	}
	assertRegisterSwitches(t, false)

	// DB 里确实被写入了 true，说明被阻止的是「覆盖运行时」而不是「写库」。
	var stored Option
	if err := DB.First(&stored, "key = ?", "RegisterEnabled").Error; err != nil {
		t.Fatalf("read back option: %v", err)
	}
	if stored.Value != "true" {
		t.Fatalf("stored option = %q, want true (DB write should still succeed)", stored.Value)
	}
}

// TestRegisterEnabledDBOptionWithoutEnv 验证未设置 env 时原语义不变：
// 数据库 option 仍然可以关闭注册。
// 版本: v0.0.28
// 日期: 2026-11-14
func TestRegisterEnabledDBOptionWithoutEnv(t *testing.T) {
	t.Setenv("REGISTER_ENABLED", "")
	t.Setenv("PASSWORD_REGISTER_ENABLED", "")
	t.Cleanup(func() {
		config.ReloadConfig()
	})
	config.ReloadConfig()
	setupOptionEnvTestDB(t)

	for _, key := range []string{"RegisterEnabled", "PasswordRegisterEnabled"} {
		if err := DB.Create(&Option{Key: key, Value: "false"}).Error; err != nil {
			t.Fatalf("seed DB option %s: %v", key, err)
		}
	}
	InitOptionMap()
	assertRegisterSwitches(t, false)
}
