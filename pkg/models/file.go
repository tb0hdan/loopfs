package models

import "time"

// FileInfo represents file metadata.
type FileInfo struct {
	Hash           string    `json:"hash"`
	Size           int64     `json:"size"`
	CreatedAt      time.Time `json:"created_at"`
	SpaceUsed      uint64    `json:"space_used,omitempty"`
	SpaceAvailable uint64    `json:"space_available,omitempty"`
}
