package models

import "time"

// Bucket represents a logical container for objects.
type Bucket struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	OwnerID    string    `json:"owner_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	IsPublic   bool      `json:"is_public"`
	QuotaBytes int64     `json:"quota_bytes,omitempty"`

	// Computed fields (not stored in database).
	ObjectCount int64 `json:"object_count,omitempty"`
	TotalSize   int64 `json:"total_size,omitempty"`
}

// BucketObject represents a named object within a bucket.
type BucketObject struct {
	ID          int64             `json:"id"`
	BucketID    int64             `json:"-"`
	Key         string            `json:"key"`
	Hash        string            `json:"hash"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ObjectListResponse represents a paginated list of objects.
type ObjectListResponse struct {
	Objects        []BucketObject `json:"objects"`
	NextCursor     string         `json:"next_cursor,omitempty"`
	IsTruncated    bool           `json:"is_truncated"`
	Prefix         string         `json:"prefix,omitempty"`
	Delimiter      string         `json:"delimiter,omitempty"`
	CommonPrefixes []string       `json:"common_prefixes,omitempty"`
}

// BucketUploadResponse extends UploadResponse with bucket info.
type BucketUploadResponse struct {
	Hash   string `json:"hash"`
	Key    string `json:"key"`
	Bucket string `json:"bucket"`
	Size   int64  `json:"size"`
}

// BucketListResponse represents a list of buckets.
type BucketListResponse struct {
	Buckets []Bucket `json:"buckets"`
}
