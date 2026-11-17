package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSiliconFlowBaseURL = "https://api.siliconflow.cn/v1"
	// DefaultSiliconFlowModel 需与硅基流动模型广场 ID 完全一致，可按需改成广场上的其他对话模型。
	DefaultSiliconFlowModel = "deepseek-ai/DeepSeek-V3"
)

// Config holds runtime settings for a persistent personal agent bot.
type Config struct {
	BotID           string
	BotName         string
	BotPublicURL    string
	Host            string
	Port            int
	DataDir         string
	SQLitePath      string
	DocsRoot        string
	ResumeRoot      string
	A2ASharedSecret string
	ModelType       string
	OpenAIAPIKey    string
	OpenAIModel     string
	OpenAIBaseURL   string
	OpenAIByAzure   bool
	ArkAPIKey       string
	ArkModel        string
	ArkBaseURL      string
	GitHubToken     string
	ASRBaseURL      string
	ASRAPIKey       string
	ASRModel        string
	MaxIterations   int
	SessionTTL      time.Duration
}

func Load() (*Config, error) {
	modelType := strings.ToLower(envOr("MODEL_TYPE", "siliconflow"))
	cfg := &Config{
		BotID:           envOr("BOT_ID", "bot-local-1"),
		BotName:         envOr("BOT_NAME", "PersonalAgent"),
		BotPublicURL:    envOr("BOT_PUBLIC_URL", "http://127.0.0.1:12120"),
		Host:            envOr("HOST", "127.0.0.1"),
		Port:            envInt("PORT", 12120),
		DataDir:         envOr("DATA_DIR", "./data"),
		DocsRoot:        envOr("BOT_DOCS_ROOT", "./data/docs"),
		ResumeRoot:      envOr("BOT_RESUME_ROOT", "./data/resumes"),
		A2ASharedSecret: envOr("A2A_SHARED_SECRET", "dev-a2a-secret-change-me"),
		ModelType:       modelType,
		OpenAIByAzure:   os.Getenv("OPENAI_BY_AZURE") == "true",
		ArkAPIKey:       os.Getenv("ARK_API_KEY"),
		ArkModel:        os.Getenv("ARK_MODEL"),
		ArkBaseURL:      os.Getenv("ARK_BASE_URL"),
		GitHubToken:     firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN")),
		MaxIterations:   envInt("MAX_ITERATIONS", 16),
		SessionTTL:      time.Duration(envInt("SESSION_TTL_HOURS", 168)) * time.Hour,
	}
	cfg.SQLitePath = envOr("SQLITE_PATH", cfg.DataDir+"/bot.db")
	applyModelDefaults(cfg)

	cfg.ASRBaseURL = envOr("ASR_BASE_URL", "")
	cfg.ASRAPIKey = envOr("ASR_API_KEY", cfg.OpenAIAPIKey)
	cfg.ASRModel = envOr("ASR_MODEL", "FunAudioLLM/SenseVoiceSmall")
	if cfg.ASRBaseURL == "" {
		if cfg.OpenAIBaseURL != "" {
			cfg.ASRBaseURL = strings.TrimRight(cfg.OpenAIBaseURL, "/")
		} else {
			cfg.ASRBaseURL = DefaultSiliconFlowBaseURL
		}
	}

	switch cfg.ModelType {
	case "siliconflow", "openai":
		if cfg.OpenAIAPIKey == "" {
			if cfg.ModelType == "siliconflow" {
				return nil, fmt.Errorf("SILICONFLOW_API_KEY（或 OPENAI_API_KEY）is required when MODEL_TYPE=%s", cfg.ModelType)
			}
			return nil, fmt.Errorf("OPENAI_API_KEY is required when MODEL_TYPE=openai")
		}
	case "ark":
		if cfg.ArkAPIKey == "" {
			return nil, fmt.Errorf("ARK_API_KEY is required when MODEL_TYPE=ark")
		}
	default:
		return nil, fmt.Errorf("unsupported MODEL_TYPE=%q (use siliconflow|openai|ark)", cfg.ModelType)
	}
	return cfg, nil
}

func applyModelDefaults(cfg *Config) {
	switch cfg.ModelType {
	case "siliconflow":
		cfg.OpenAIAPIKey = firstNonEmpty(os.Getenv("SILICONFLOW_API_KEY"), os.Getenv("OPENAI_API_KEY"))
		cfg.OpenAIModel = firstNonEmpty(os.Getenv("SILICONFLOW_MODEL"), os.Getenv("OPENAI_MODEL"), DefaultSiliconFlowModel)
		cfg.OpenAIBaseURL = firstNonEmpty(os.Getenv("SILICONFLOW_BASE_URL"), os.Getenv("OPENAI_BASE_URL"), DefaultSiliconFlowBaseURL)
	case "openai":
		cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
		cfg.OpenAIModel = envOr("OPENAI_MODEL", "gpt-4o-mini")
		cfg.OpenAIBaseURL = os.Getenv("OPENAI_BASE_URL")
	}
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// UsesOpenAICompatible reports whether chat goes through the OpenAI-compatible client.
func (c *Config) UsesOpenAICompatible() bool {
	return c.ModelType == "siliconflow" || c.ModelType == "openai"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
