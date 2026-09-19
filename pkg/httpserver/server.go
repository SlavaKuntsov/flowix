// Package httpserver — общий bootstrap HTTP-сервера Go-сервисов Flowix:
// таймауты http.Server + graceful shutdown по SIGTERM/SIGINT (issue #63).
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	DefaultReadHeaderTimeout = 15 * time.Second
	DefaultReadTimeout       = 15 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownGrace     = 30 * time.Second
)

// Timeouts задаёт таймауты http.Server. Нулевые ReadTimeout/WriteTimeout
// означают «без лимита» — так нужно сервисам, через которые стримятся
// большие тела запросов (upload/gateway: тела до 5 ГБ не влезают в
// Read/Write timeout — иначе деплой/таймаут обрывает in-flight загрузки).
type Timeouts struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownGrace     time.Duration
}

// Defaults — литеральные значения issue #63: Read 15s, Write 60s, Idle 120s.
// Подходит сервисам с маленькими JSON-телами (metadata).
func Defaults() Timeouts {
	return Timeouts{
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ShutdownGrace:     DefaultShutdownGrace,
	}
}

// Streaming — таймауты для сервисов, проксирующих/принимающих большие тела
// (upload 5 ГБ): лимитируем только заголовки, тело стримим без лимита.
func Streaming() Timeouts {
	return Timeouts{
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		ShutdownGrace:     DefaultShutdownGrace,
	}
}

// Run запускает http.Server и блокируется до SIGTERM/SIGINT или ошибки.
// Shutdown ждёт завершения in-flight запросов (загрузки не обрываются),
// но не дольше ShutdownGrace. SHUTDOWN_GRACE (например «4m») переопределяет
// грейс из env — должен быть меньше stop_grace_period в docker-compose.
func Run(addr string, h http.Handler, t Timeouts) error {
	if t.ShutdownGrace <= 0 {
		t.ShutdownGrace = DefaultShutdownGrace
	}
	if v := os.Getenv("SHUTDOWN_GRACE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			t.ShutdownGrace = d
		}
	}
	srv := newServer(addr, h, t)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-quit:
	}

	log.Info().Dur("grace", t.ShutdownGrace).Msg("shutdown signal received, waiting for in-flight requests")
	ctx, cancel := context.WithTimeout(context.Background(), t.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}

func newServer(addr string, h http.Handler, t Timeouts) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: t.ReadHeaderTimeout,
		ReadTimeout:       t.ReadTimeout,
		WriteTimeout:      t.WriteTimeout,
		IdleTimeout:       t.IdleTimeout,
	}
}
