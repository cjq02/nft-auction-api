package config

type Logger interface {
	Info(format string, args ...interface{})
}

type LogLevel string

const (
	LogLevelError LogLevel = "error"
	LogLevelWarn  LogLevel = "warn"
	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
)

type AppConfig struct {
	LogLevel         LogLevel
	EnableDetailLogs bool
}

func NewAppConfig(logger Logger) *AppConfig {
	logLevel := LogLevel(getEnv("LOG_LEVEL", "info"))
	enableDetailLogs := getEnv("ENABLE_DETAIL_LOGS", "true") == "true"

	return &AppConfig{
		LogLevel:         logLevel,
		EnableDetailLogs: enableDetailLogs,
	}
}

func (c *AppConfig) ShouldLogDetails() bool {
	return c.EnableDetailLogs
}
