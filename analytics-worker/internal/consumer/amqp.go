package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/linkr/analytics-worker/internal/repo"
)

const (
	exchangeName = "redirects"
	exchangeType = "topic"
	queueName    = "analytics.clicks"
	routingKey   = "redirect.clicked"
	consumerTag  = "analytics-worker"
	maxRetries   = 5
	baseDelay    = time.Second
	maxDelay     = 30 * time.Second
)

var errStopped = errors.New("consumer stopped")

type AMQPConsumer struct {
	url      string
	prefetch int
	repo     repo.ClickRepository
	log      *slog.Logger
	conn     *amqp.Connection
	ch       *amqp.Channel
	stopCh   chan struct{}
	doneCh   chan struct{}
	once     sync.Once
}

func NewAMQPConsumer(url string, prefetch int, r repo.ClickRepository, log *slog.Logger) *AMQPConsumer {
	return &AMQPConsumer{
		url:      url,
		prefetch: prefetch,
		repo:     r,
		log:      log,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (c *AMQPConsumer) Start(ctx context.Context) error {
	deliveries, err := c.dial()
	if err != nil {
		return err
	}
	c.log.Info("amqp consumer started")
	go c.runLoop(deliveries)
	return nil
}

func (c *AMQPConsumer) Stop() error {
	c.once.Do(func() { close(c.stopCh) })
	<-c.doneCh
	if c.ch != nil {
		_ = c.ch.Close()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.log.Info("amqp consumer stopped")
	return nil
}

func (c *AMQPConsumer) IsAlive() bool {
	return c.conn != nil && !c.conn.IsClosed()
}

func (c *AMQPConsumer) runLoop(deliveries <-chan amqp.Delivery) {
	defer close(c.doneCh)

	for {
		chClose := c.ch.NotifyClose(make(chan *amqp.Error, 1))
		disconnected := false

		for !disconnected {
			select {
			case <-c.stopCh:
				return
			case amqpErr := <-chClose:
				if amqpErr != nil {
					c.log.Warn("amqp channel closed", "error", amqpErr)
				}
				disconnected = true
			case d, ok := <-deliveries:
				if !ok {
					disconnected = true
				} else {
					c.ProcessMessage(d)
				}
			}
		}

		select {
		case <-c.stopCh:
			return
		default:
		}

		c.log.Warn("amqp disconnected, reconnecting")
		var err error
		deliveries, err = c.dial()
		if err != nil {
			if !errors.Is(err, errStopped) {
				c.log.Error("amqp reconnect exhausted, consumer stopping", "error", err)
			}
			return
		}
	}
}

// ProcessMessage is exported to allow direct unit testing without a live broker.
func (c *AMQPConsumer) ProcessMessage(d amqp.Delivery) {
	var p struct {
		Code      string `json:"code"`
		Timestamp string `json:"timestamp"`
		Referrer  string `json:"referrer"`
		IPHash    string `json:"ip_hash"`
	}
	if err := json.Unmarshal(d.Body, &p); err != nil {
		c.log.Warn("amqp nack: invalid JSON", "error", err)
		_ = d.Nack(false, false)
		return
	}
	if p.Code == "" {
		c.log.Warn("amqp nack: empty code")
		_ = d.Nack(false, false)
		return
	}
	ts, err := time.Parse(time.RFC3339, p.Timestamp)
	if err != nil {
		c.log.Warn("amqp nack: invalid timestamp", "timestamp", p.Timestamp, "error", err)
		_ = d.Nack(false, false)
		return
	}
	event := repo.ClickEvent{
		Code:       p.Code,
		Timestamp:  ts,
		Referrer:   p.Referrer,
		IPHash:     p.IPHash,
		ReceivedAt: time.Now(),
	}
	if err := c.repo.Insert(context.Background(), event); err != nil {
		c.log.Warn("amqp nack: mongo insert failed", "code", p.Code, "error", err)
		_ = d.Nack(false, false)
		return
	}
	latencyMs := time.Since(d.Timestamp).Milliseconds()
	c.log.Info("click recorded", "code", p.Code, "timestamp", p.Timestamp, "latency_ms", latencyMs)
	_ = d.Ack(false)
}

func (c *AMQPConsumer) dial() (<-chan amqp.Delivery, error) {
	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := min(baseDelay*time.Duration(1<<uint(attempt-1)), maxDelay)
			c.log.Warn("amqp connect attempt", "attempt", attempt, "of", maxRetries, "delay", delay)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-c.stopCh:
				timer.Stop()
				return nil, errStopped
			}
		}
		deliveries, err := c.tryConnect()
		if err == nil {
			return deliveries, nil
		}
		lastErr = err
		c.log.Warn("amqp connect failed", "attempt", attempt+1, "of", maxRetries, "error", err)
	}
	return nil, fmt.Errorf("amqp connect failed after %d attempts: %w", maxRetries, lastErr)
}

func (c *AMQPConsumer) tryConnect() (<-chan amqp.Delivery, error) {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := ch.ExchangeDeclare(exchangeName, exchangeType, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, routingKey, exchangeName, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Qos(c.prefetch, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	deliveries, err := ch.Consume(queueName, consumerTag, false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	c.conn = conn
	c.ch = ch
	c.log.Info("amqp connected")
	return deliveries, nil
}
