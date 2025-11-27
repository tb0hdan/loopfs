package models

import "time"

// BackendStatus represents the health status of a backend server.
type BackendStatus struct {
	URL           string    `json:"url"`
	Online        bool      `json:"online"`
	LastCheck     time.Time `json:"last_check"`
	LastError     string    `json:"last_error,omitempty"`
	Latency       int64     `json:"latency_ms"`
	ConsecFails   int       `json:"consecutive_failures"`
	NodeInfo      *NodeInfo `json:"node_info,omitempty"`
	AvailableSpace uint64   `json:"available_space"`
}
