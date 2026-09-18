// Package metrics — общие Prometheus-метрики Go-сервисов Flowix
// (issue #63: единый набор collectors вместо копий в каждом сервисе).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	UploadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upload_bytes",
		Help: "Total uploaded bytes via gateway/upload",
	})
	RabbitMQQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_depth",
		Help: "Current depth of video.uploaded queue",
	})
	VodCacheHit = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vod_cache_hit",
		Help: "Number of VOD cache hits",
	})
	FfmpegDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ffmpeg_duration_seconds",
		Help:    "FFmpeg transcoding duration",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(UploadBytes, RabbitMQQueueDepth, VodCacheHit, FfmpegDuration)
}

// Handler — /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware — счётчик VodCacheHit на каждый запрос (используется gateway
// на /hls/*).
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		VodCacheHit.Inc()
		next.ServeHTTP(w, r)
	})
}
