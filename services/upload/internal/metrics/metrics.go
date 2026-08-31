package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	UploadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upload_bytes",
		Help: "Total uploaded bytes",
	})
	RabbitMQQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_depth",
		Help: "RabbitMQ queue depth",
	})
	VodCacheHit = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vod_cache_hit",
		Help: "VOD cache hits",
	})
	FfmpegDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ffmpeg_duration_seconds",
		Help:    "FFmpeg duration",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(UploadBytes, RabbitMQQueueDepth, VodCacheHit, FfmpegDuration)
}

func Handler() http.Handler {
	return promhttp.Handler()
}
