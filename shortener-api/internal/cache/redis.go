package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(addr string) *RedisCache {
	return &RedisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (c *RedisCache) Get(ctx context.Context, code string) (string, error) {
	val, err := c.client.Get(ctx, code).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return val, err
}

func (c *RedisCache) Set(ctx context.Context, code string, longURL string, ttl time.Duration) error {
	return c.client.Set(ctx, code, longURL, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, code string) error {
	return c.client.Del(ctx, code).Err()
}

func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
