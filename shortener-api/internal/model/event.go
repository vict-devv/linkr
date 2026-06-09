package model

import "time"

type RedirectEvent struct {
	Code      string    `json:"code"`
	Timestamp time.Time `json:"timestamp"`
	Referrer  string    `json:"referrer"`
	IPHash    string    `json:"ip_hash"`
}
