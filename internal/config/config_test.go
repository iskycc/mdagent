package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("expected default port 8080, got %s", cfg.Port)
	}
	if cfg.OpenAIBaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("unexpected base url: %s", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIModel != "deepseek-chat" {
		t.Errorf("unexpected model: %s", cfg.OpenAIModel)
	}
	if cfg.DBMaxOpen != 50 {
		t.Errorf("unexpected DBMaxOpen: %d", cfg.DBMaxOpen)
	}
	if cfg.ModelMaxTokens != 8192 {
		t.Errorf("unexpected ModelMaxTokens: %d", cfg.ModelMaxTokens)
	}
	if cfg.MaxOutputTokens != 1024 {
		t.Errorf("unexpected MaxOutputTokens: %d", cfg.MaxOutputTokens)
	}
	if cfg.MiaodiAPIBaseURL != "https://api.libv.cc/miaodi" {
		t.Errorf("unexpected MiaodiAPIBaseURL: %s", cfg.MiaodiAPIBaseURL)
	}
	if cfg.MiaodiMailAPIURL != "https://api.miaodiapp.com/api/newmail.php" {
		t.Errorf("unexpected MiaodiMailAPIURL: %s", cfg.MiaodiMailAPIURL)
	}
	if cfg.MiaodiPictureAPIURL != "https://picture.miaodiapp.com/api/upload" {
		t.Errorf("unexpected MiaodiPictureAPIURL: %s", cfg.MiaodiPictureAPIURL)
	}
}

func TestLoad_Overrides(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DB_MAX_OPEN", "100")
	os.Setenv("OPENAI_MODEL", "gpt-4")
	os.Setenv("OPENAI_MODEL_MAX_TOKENS", "4096")
	os.Setenv("OPENAI_MAX_OUTPUT_TOKENS", "512")
	os.Setenv("MIAODI_API_BASE_URL", "https://api.example.com/miaodi")
	os.Setenv("MIAODI_MAIL_API_URL", "https://mail.example.com")
	os.Setenv("MIAODI_PICTURE_API_URL", "https://pic.example.com")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("DB_MAX_OPEN")
	defer os.Unsetenv("OPENAI_MODEL")
	defer os.Unsetenv("OPENAI_MODEL_MAX_TOKENS")
	defer os.Unsetenv("OPENAI_MAX_OUTPUT_TOKENS")
	defer os.Unsetenv("MIAODI_API_BASE_URL")
	defer os.Unsetenv("MIAODI_MAIL_API_URL")
	defer os.Unsetenv("MIAODI_PICTURE_API_URL")

	cfg := Load()
	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
	if cfg.DBMaxOpen != 100 {
		t.Errorf("expected DBMaxOpen 100, got %d", cfg.DBMaxOpen)
	}
	if cfg.OpenAIModel != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", cfg.OpenAIModel)
	}
	if cfg.ModelMaxTokens != 4096 {
		t.Errorf("expected ModelMaxTokens 4096, got %d", cfg.ModelMaxTokens)
	}
	if cfg.MaxOutputTokens != 512 {
		t.Errorf("expected MaxOutputTokens 512, got %d", cfg.MaxOutputTokens)
	}
	if cfg.MiaodiAPIBaseURL != "https://api.example.com/miaodi" {
		t.Errorf("expected MiaodiAPIBaseURL override, got %s", cfg.MiaodiAPIBaseURL)
	}
	if cfg.MiaodiMailAPIURL != "https://mail.example.com" {
		t.Errorf("expected MiaodiMailAPIURL override, got %s", cfg.MiaodiMailAPIURL)
	}
	if cfg.MiaodiPictureAPIURL != "https://pic.example.com" {
		t.Errorf("expected MiaodiPictureAPIURL override, got %s", cfg.MiaodiPictureAPIURL)
	}
}

func TestLoad_InvalidIntFallsBack(t *testing.T) {
	os.Setenv("DB_MAX_OPEN", "not-a-number")
	defer os.Unsetenv("DB_MAX_OPEN")
	cfg := Load()
	if cfg.DBMaxOpen != 50 {
		t.Errorf("expected fallback 50, got %d", cfg.DBMaxOpen)
	}
}

func TestValidate(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty config")
	}
	cfg.OpenAIAPIKey = "sk-test"
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error missing db")
	}
	cfg.DBUser = "root"
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error missing db name")
	}
	cfg.DBName = "test"
	cfg.ModelMaxTokens = 8192
	cfg.MaxOutputTokens = 1024
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
	cfg.MaxOutputTokens = 8192
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for invalid token budget")
	}
}

func TestValidate_CallbackAuthEnabledRequiresSecret(t *testing.T) {
	cfg := &Config{
		CallbackAuthEnabled: true,
		OpenAIAPIKey:        "sk-test",
		DBUser:              "root",
		DBName:              "test",
		ModelMaxTokens:      8192,
		MaxOutputTokens:     1024,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when callback auth enabled without secret")
	}
	cfg.CallbackSecret = "secret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_ProductionWithoutCallbackAuth(t *testing.T) {
	cfg := &Config{
		Env:             "production",
		OpenAIAPIKey:    "sk-test",
		DBUser:          "root",
		DBName:          "test",
		ModelMaxTokens:  8192,
		MaxOutputTokens: 1024,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected validation to pass when callback auth is disabled: %v", err)
	}
}

func TestDSN(t *testing.T) {
	cfg := &Config{
		DBUser: "u",
		DBPass: "p",
		DBHost: "h",
		DBPort: "3306",
		DBName: "db",
	}
	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("DSN is empty")
	}
	for _, want := range []string{"loc=Asia%2FShanghai", "time_zone=%27%2B08%3A00%27"} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("expected DSN to contain %s, got %s", want, dsn)
		}
	}
}
