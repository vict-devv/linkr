package repo

import (
	"context"
	"time"
)

type ClickEvent struct {
	Code       string
	Timestamp  time.Time
	Referrer   string
	IPHash     string
	ReceivedAt time.Time
}

type ClickRepository interface {
	Insert(ctx context.Context, event ClickEvent) error
}
