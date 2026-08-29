package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// TestPublisherReconnect verifies the scenario that broke the e2e run:
// the broker drops the connection, and the next Publish must transparently
// reconnect instead of returning 504 "channel/connection is not open".
//
// Requires a live broker; set RABBITMQ_URL (e.g. amqp://flowix:flowix@localhost:5672/).
// Skipped otherwise so `make test-go` stays green on a host without RabbitMQ.
func TestPublisherReconnect(t *testing.T) {
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL not set; skipping broker integration test")
	}
	const queue = "flowix.test.publisher.reconnect"

	pub, err := NewPublisher(url, queue)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer pub.Close()

	purge(t, url, queue)
	ctx := context.Background()

	if err := pub.Publish(ctx, map[string]string{"n": "1"}); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	// Simulate a broker-side drop: kill the underlying connection.
	_ = pub.conn.Close()
	if !pub.conn.IsClosed() {
		t.Fatal("connection should report closed after Close()")
	}

	// This publish must succeed by reconnecting under the hood.
	if err := pub.Publish(ctx, map[string]string{"n": "2"}); err != nil {
		t.Fatalf("publish after drop should reconnect, got: %v", err)
	}

	got := drain(t, url, queue, 2)
	if got["1"] != 1 || got["2"] != 1 {
		t.Fatalf("expected both messages delivered, got %v", got)
	}
}

// purge empties the queue so the test starts from a known state.
func purge(t *testing.T, url, queue string) {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("purge dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("purge channel: %v", err)
	}
	defer func() { _ = ch.Close() }()
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatalf("purge declare: %v", err)
	}
	if _, err := ch.QueuePurge(queue, false); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

// drain pulls up to n messages and returns a count of each payload's "n" field.
func drain(t *testing.T, url, queue string, n int) map[string]int {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("drain dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("drain channel: %v", err)
	}
	defer func() { _ = ch.Close() }()

	got := map[string]int{}
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < n && time.Now().Before(deadline) {
		msg, ok, err := ch.Get(queue, true)
		if err != nil {
			t.Fatalf("drain get: %v", err)
		}
		if !ok {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var p map[string]string
		if err := json.Unmarshal(msg.Body, &p); err != nil {
			t.Fatalf("drain unmarshal %q: %v", msg.Body, err)
		}
		got[p["n"]]++
	}
	return got
}
