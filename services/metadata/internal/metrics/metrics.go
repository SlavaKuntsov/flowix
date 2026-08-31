package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RabbitMQQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_depth",
		Help: "RabbitMQ queue depth",
	})
	VodCacheHit = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vod_cache_hit",
		Help: "VOD cache hits",
	})
	UploadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upload_bytes",
		Help: "Uploaded bytes",
	})
	FfmpegDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ffmpeg_duration_seconds",
		Help:    "FFmpeg duration",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(RabbitMQQueueDepth, VodCacheHit, UploadBytes, FfmpegDuration)
}

func Handler() http.Handler {
	return promhttp.Handler()
}
