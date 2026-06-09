package consumer

import "context"

type EventConsumer interface {
	Start(ctx context.Context) error
	Stop() error
}
