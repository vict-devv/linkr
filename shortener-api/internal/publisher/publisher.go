package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/linkr/shortener-api/internal/model"
)

const (
	exchangeName = "redirects"
	exchangeType = "topic"
	routingKey   = "redirect.clicked"
	maxRetries   = 5
	baseDelay    = time.Second
	maxDelay     = 30 * time.Second
)

var errStopped = errors.New("publisher stopped")

// EventPublisher publishes redirect events to a message broker.
type EventPublisher interface {
	Publish(ctx context.Context, event model.RedirectEvent) error
	Close() error
}

// AMQPPublisher publishes RedirectEvents to RabbitMQ.
type AMQPPublisher struct {
	url    string
	log    *slog.Logger
	conn   *amqp.Connection
	ch     *amqp.Channel
	mu     sync.RWMutex
	stopCh chan struct{}
	doneCh chan struct{}
	once   sync.Once
}

func NewAMQPPublisher(url string, log *slog.Logger) *AMQPPublisher {
	return &AMQPPublisher{
		url:    url,
		log:    log,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Connect starts the background connection loop. It is non-fatal: the service
// starts even if RabbitMQ is unreachable; IsAlive() returns false until a
// connection is established.
func (p *AMQPPublisher) Connect() {
	go p.runLoop()
}

// IsAlive reports whether the AMQP connection is currently open.
func (p *AMQPPublisher) IsAlive() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conn != nil && !p.conn.IsClosed()
}

// Publish serialises event to JSON and publishes it to the redirects exchange.
// Returns an error if the connection is not available or the publish fails;
// callers should treat errors as non-fatal.
func (p *AMQPPublisher) Publish(ctx context.Context, event model.RedirectEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	p.mu.RLock()
	ch := p.ch
	p.mu.RUnlock()

	if ch == nil {
		return errors.New("amqp not connected")
	}

	return ch.PublishWithContext(ctx, exchangeName, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

// Close signals the connection loop to stop and waits for it to exit before
// closing the AMQP channel and connection.
func (p *AMQPPublisher) Close() error {
	p.once.Do(func() { close(p.stopCh) })
	<-p.doneCh
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.log.Info("amqp publisher stopped")
	return nil
}

func (p *AMQPPublisher) runLoop() {
	defer close(p.doneCh)

	for {
		if err := p.dial(); err != nil {
			if !errors.Is(err, errStopped) {
				p.log.Error("amqp publisher connect exhausted", "error", err)
			}
			return
		}

		p.mu.RLock()
		chClose := p.ch.NotifyClose(make(chan *amqp.Error, 1))
		p.mu.RUnlock()

		select {
		case <-p.stopCh:
			return
		case amqpErr := <-chClose:
			if amqpErr != nil {
				p.log.Warn("amqp channel closed", "error", amqpErr)
			}
		}

		select {
		case <-p.stopCh:
			return
		default:
		}

		p.log.Warn("amqp disconnected, reconnecting")
	}
}

func (p *AMQPPublisher) dial() error {
	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := min(baseDelay*time.Duration(1<<uint(attempt-1)), maxDelay)
			p.log.Warn("amqp connect attempt", "attempt", attempt, "of", maxRetries, "delay", delay)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-p.stopCh:
				timer.Stop()
				return errStopped
			}
		}
		if err := p.tryConnect(); err == nil {
			return nil
		} else {
			lastErr = err
			p.log.Warn("amqp connect failed", "attempt", attempt+1, "of", maxRetries, "error", err)
		}
	}
	return fmt.Errorf("amqp connect failed after %d attempts: %w", maxRetries, lastErr)
}

func (p *AMQPPublisher) tryConnect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := ch.ExchangeDeclare(exchangeName, exchangeType, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	p.mu.Lock()
	p.conn = conn
	p.ch = ch
	p.mu.Unlock()
	p.log.Info("amqp connected")
	return nil
}
