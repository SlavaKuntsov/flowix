package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

type Publisher struct {
	url   string
	queue string

	mu      sync.Mutex
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewPublisher(url, queue string) (*Publisher, error) {
	p := &Publisher{url: url, queue: queue}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

const (
	dlxExchange = "dlx"
	retryTTL    = int32(30000)
)

// connect dials a fresh connection + channel and declares the queue topology.
// For video.uploaded it creates DLX + DLQ + retry queue (TTL 30s) per Phase 10b.
func (p *Publisher) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("amqp channel: %w", err)
	}
	if err := declareTopology(ch, p.queue); err != nil {
		// If queue exists with different args (pre-10b without DLX), broker returns 406.
		// Auto-heal by deleting stale queue and re-declaring (only for video.uploaded).
		if strings.Contains(err.Error(), "PRECONDITION") || strings.Contains(err.Error(), "inequivalent") {
			log.Warn().Err(err).Str("queue", p.queue).Msg("queue args mismatch, recreating with DLX")
			_ = ch.Close()
			_ = conn.Close()
			// delete stale queue via temp connection
			if tmpConn, te := amqp.Dial(p.url); te == nil {
				if tmpCh, ce := tmpConn.Channel(); ce == nil {
					_, _ = tmpCh.QueueDelete(p.queue, false, false, false)
					_ = tmpCh.Close()
				}
				_ = tmpConn.Close()
			}
			// retry fresh
			conn2, err2 := amqp.Dial(p.url)
			if err2 != nil {
				return fmt.Errorf("amqp dial retry: %w", err2)
			}
			ch2, err2 := conn2.Channel()
			if err2 != nil {
				_ = conn2.Close()
				return fmt.Errorf("amqp channel retry: %w", err2)
			}
			if err2 := declareTopology(ch2, p.queue); err2 != nil {
				_ = ch2.Close()
				_ = conn2.Close()
				return err2
			}
			p.conn = conn2
			p.channel = ch2
			log.Info().Str("queue", p.queue).Msg("rabbitmq publisher ready (recreated)")
			return nil
		}
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	p.conn = conn
	p.channel = ch
	log.Info().Str("queue", p.queue).Msg("rabbitmq publisher ready")
	return nil
}

func declareTopology(ch *amqp.Channel, queue string) error {
	// Only video.uploaded gets DLX/DLQ/retry; other queues stay simple.
	if queue != "video.uploaded" {
		if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
			return fmt.Errorf("queue declare: %w", err)
		}
		return nil
	}
	if err := ch.ExchangeDeclare(dlxExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("exchange declare dlx: %w", err)
	}
	dlq := queue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("dlq declare: %w", err)
	}
	if err := ch.QueueBind(dlq, dlq, dlxExchange, false, nil); err != nil {
		return fmt.Errorf("dlq bind: %w", err)
	}
	retryQ := queue + ".retry"
	retryArgs := amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": queue,
		"x-message-ttl":             retryTTL,
	}
	if _, err := ch.QueueDeclare(retryQ, true, false, false, false, retryArgs); err != nil {
		return fmt.Errorf("retry queue declare: %w", err)
	}
	mainArgs := amqp.Table{
		"x-dead-letter-exchange":    dlxExchange,
		"x-dead-letter-routing-key": dlq,
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, mainArgs); err != nil {
		// If queue already exists without DLX args, broker returns 406 PRECONDITION_FAILED.
		// Caller must purge the old queue (rabbitmqadmin delete queue) before redeploy.
		return fmt.Errorf("queue declare: %w", err)
	}
	// Phase 12: fan-out per-rendition queues for parallel workers (prefetch 1 each)
	for _, fq := range []string{"video.transcode.360p", "video.transcode.720p", "video.transcode.1080p"} {
		if _, err := ch.QueueDeclare(fq, true, false, false, false, mainArgs); err != nil {
			return fmt.Errorf("fanout queue declare %s: %w", fq, err)
		}
	}
	return nil
}

// ensure re-establishes the connection/channel if either has been closed
// (broker restart, heartbeat timeout, prior channel error). Caller must hold p.mu.
func (p *Publisher) ensure() error {
	if p.conn != nil && !p.conn.IsClosed() && p.channel != nil && !p.channel.IsClosed() {
		return nil
	}
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	log.Warn().Str("queue", p.queue).Msg("rabbitmq connection lost, reconnecting")
	return p.connect()
}

func (p *Publisher) Publish(ctx context.Context, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensure(); err != nil {
		return err
	}
	if err := p.publish(ctx, body); err == nil {
		return nil
	} else {
		// The channel may have died between ensure() and publish. Reconnect once and retry.
		log.Warn().Err(err).Str("queue", p.queue).Msg("publish failed, retrying after reconnect")
	}
	if err := p.ensure(); err != nil {
		return err
	}
	return p.publish(ctx, body)
}

func (p *Publisher) publish(ctx context.Context, body []byte) error {
	return p.channel.PublishWithContext(ctx, "", p.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}
