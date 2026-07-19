package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 保存所有配置
type Config struct {
	Port            string
	DBHost          string
	DBPort          string
	DBUser          string
	DBPass          string
	DBName          string
	DBMaxOpen       int
	DBMaxIdle       int
	OpenAIAPIKey    string
	OpenAIBaseURL   string
	OpenAIModel     string
	ModelMaxTokens  int
	MaxOutputTokens int
	CallbackPath    string
}

// Load 从环境变量加载配置
func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          getEnv("DB_PORT", "3306"),
		DBUser:          getEnv("DB_USER", "root"),
		DBPass:          getEnv("DB_PASS", ""),
		DBName:          getEnv("DB_NAME", "miaodi_agent"),
		DBMaxOpen:       getEnvInt("DB_MAX_OPEN", 50),
		DBMaxIdle:       getEnvInt("DB_MAX_IDLE", 10),
		OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL:   getEnv("OPENAI_BASE_URL", "https://api.deepseek.com/v1"),
		OpenAIModel:     getEnv("OPENAI_MODEL", "deepseek-chat"),
		ModelMaxTokens:  getEnvInt("OPENAI_MODEL_MAX_TOKENS", 8192),
		MaxOutputTokens: getEnvInt("OPENAI_MAX_OUTPUT_TOKENS", 1024),
		CallbackPath:    getEnv("CALLBACK_PATH", "/callback"),
	}
}

// DSN 返回 MySQL 连接串
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Asia%%2FShanghai&time_zone=%%27%%2B08%%3A00%%27&interpolateParams=true",
		c.DBUser, c.DBPass, c.DBHost, c.DBPort, c.DBName)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

// Validate 检查必填配置
func (c *Config) Validate() error {
	if c.OpenAIAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	if c.DBUser == "" || c.DBName == "" {
		return fmt.Errorf("DB_USER and DB_NAME are required")
	}
	if c.ModelMaxTokens <= 0 {
		return fmt.Errorf("OPENAI_MODEL_MAX_TOKENS must be positive")
	}
	if c.MaxOutputTokens <= 0 {
		return fmt.Errorf("OPENAI_MAX_OUTPUT_TOKENS must be positive")
	}
	if c.MaxOutputTokens >= c.ModelMaxTokens {
		return fmt.Errorf("OPENAI_MAX_OUTPUT_TOKENS must be less than OPENAI_MODEL_MAX_TOKENS")
	}
	return nil
}
