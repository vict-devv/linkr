package cache

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("cache miss")

type URLCache interface {
	Get(ctx context.Context, code string) (string, error)
	Set(ctx context.Context, code string, longURL string, ttl time.Duration) error
	Delete(ctx context.Context, code string) error
}
