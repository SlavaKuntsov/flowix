package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RabbitMQQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_depth",
		Help: "Current depth of video.uploaded queue",
	})
	VodCacheHit = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vod_cache_hit",
		Help: "Number of VOD cache hits",
	})
	UploadBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upload_bytes",
		Help: "Total uploaded bytes via gateway",
	})
	FfmpegDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ffmpeg_duration_seconds",
		Help:    "FFmpeg transcoding duration",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	prometheus.MustRegister(RabbitMQQueueDepth, VodCacheHit, UploadBytes, FfmpegDuration)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		VodCacheHit.Inc()
		next.ServeHTTP(w, r)
	})
}
