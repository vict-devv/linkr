package repo

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("url not found")

type URLRepository interface {
	Save(ctx context.Context, longURL, code string) error
	Find(ctx context.Context, code string) (string, error)
}
