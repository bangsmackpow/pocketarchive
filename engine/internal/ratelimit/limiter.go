// Package ratelimit uses Valkey for login throttling. If Valkey is unavailable
// the limiter fails open so authentication is not taken down by cache outages.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

func New(valkeyURL string, limit int64, window time.Duration) (*Limiter, error) {
	if valkeyURL == "" {
		return &Limiter{}, nil
	}
	opts, err := redis.ParseURL(valkeyURL)
	if err != nil {
		return nil, fmt.Errorf("parse VALKEY_URL: %w", err)
	}
	return &Limiter{client: redis.NewClient(opts), limit: limit, window: window}, nil
}

func (l *Limiter) Allow(ctx context.Context, key string) bool {
	if l == nil || l.client == nil {
		return true
	}
	pipe := l.client.Pipeline()
	incr := pipe.Incr(ctx, "ratelimit:"+key)
	pipe.Expire(ctx, "ratelimit:"+key, l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return true
	}
	return incr.Val() <= l.limit
}

func (l *Limiter) Reset(ctx context.Context, key string) {
	if l == nil || l.client == nil {
		return
	}
	_ = l.client.Del(ctx, "ratelimit:"+key).Err()
}

func (l *Limiter) Ping(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Ping(ctx).Err()
}

func (l *Limiter) Close() error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Close()
}
