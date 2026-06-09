package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/linkr/stats-api/internal/repo"
	"golang.org/x/sync/errgroup"
)

type statsResponse struct {
	Code           string               `json:"code"`
	TotalClicks    int64                `json:"total_clicks"`
	ClicksOverTime []repo.ClicksOverTime `json:"clicks_over_time"`
	TopReferrers   []repo.TopReferrer   `json:"top_referrers"`
}

func statsHandler(cfg Config, r repo.StatsRepository, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		code := req.PathValue("code")

		var (
			totalClicks int64
			overTime    []repo.ClicksOverTime
			referrers   []repo.TopReferrer
		)

		g, gctx := errgroup.WithContext(req.Context())
		g.Go(func() error {
			var err error
			totalClicks, err = r.TotalClicks(gctx, code)
			return err
		})
		g.Go(func() error {
			var err error
			overTime, err = r.ClicksOverTime(gctx, code, cfg.StatsWindowDays)
			return err
		})
		g.Go(func() error {
			var err error
			referrers, err = r.TopReferrers(gctx, code, cfg.TopReferrersLimit)
			return err
		})

		if err := g.Wait(); err != nil {
			log.ErrorContext(req.Context(), "stats query failed", "code", code, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		if totalClicks == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "code not found"})
			return
		}

		if referrers == nil {
			referrers = []repo.TopReferrer{}
		}

		writeJSON(w, http.StatusOK, statsResponse{
			Code:           code,
			TotalClicks:    totalClicks,
			ClicksOverTime: zeroFill(overTime, cfg.StatsWindowDays),
			TopReferrers:   referrers,
		})
	}
}

func zeroFill(data []repo.ClicksOverTime, days int) []repo.ClicksOverTime {
	byDate := make(map[string]int64, len(data))
	for _, d := range data {
		byDate[d.Date] = d.Count
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(days - 1))
	result := make([]repo.ClicksOverTime, days)
	for i := range days {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		result[i] = repo.ClicksOverTime{Date: date, Count: byDate[date]}
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
