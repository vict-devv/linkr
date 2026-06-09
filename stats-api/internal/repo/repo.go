package repo

import "context"

type ClicksOverTime struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type TopReferrer struct {
	Referrer string `json:"referrer"`
	Count    int64  `json:"count"`
}

type StatsRepository interface {
	TotalClicks(ctx context.Context, code string) (int64, error)
	ClicksOverTime(ctx context.Context, code string, days int) ([]ClicksOverTime, error)
	TopReferrers(ctx context.Context, code string, limit int) ([]TopReferrer, error)
}
