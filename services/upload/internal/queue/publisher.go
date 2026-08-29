package queue

import (
	"context"
	"encoding/json"
	"fmt"
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

// connect dials a fresh connection + channel and declares the queue.
// Caller must hold p.mu (the constructor runs before the publisher is shared).
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
	if _, err := ch.QueueDeclare(p.queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("queue declare: %w", err)
	}
	p.conn = conn
	p.channel = ch
	log.Info().Str("queue", p.queue).Msg("rabbitmq publisher ready")
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
