// Package logger — единая инициализация zerolog для Go-сервисов Flowix
// (issue #63: один bootstrap вместо копий в каждом main.go).
package logger

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup настраивает глобальный zerolog (console в dev / json в prod, уровень
// из LOG_LEVEL) и возвращает логгер с полем service=<service>.
func Setup(service string) zerolog.Logger {
	// zerolog console in dev, json in prod
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "console" || os.Getenv("ENV") == "dev" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	} else {
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		if l, err := zerolog.ParseLevel(lvl); err == nil {
			zerolog.SetGlobalLevel(l)
		}
	}
	return log.With().Timestamp().Str("service", service).Logger()
}
