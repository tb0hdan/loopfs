package bucket

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"loopfs/pkg/models"

	_ "modernc.org/sqlite"
)

// bucketNamePattern defines the valid format for bucket names.
// Bucket names must be 3-63 characters, lowercase alphanumeric, and can contain hyphens.
var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$|^[a-z0-9]{3}$`)

// Store manages bucket and object metadata in SQLite.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// BucketOptions contains optional settings for bucket creation.
type BucketOptions struct {
	IsPublic   bool
	QuotaBytes int64
}

// ListOptions contains options for listing objects.
type ListOptions struct {
	Prefix    string
	Delimiter string
	MaxKeys   int
	Cursor    string
}

// NewStore creates a new bucket store with the given database path.
func NewStore(dbPath string) (*Store, error) {
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to open database: %w", ErrDatabaseError, err)
	}

	ctx := context.Background()

	// Enable foreign keys
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("%w: failed to enable foreign keys: %w", ErrDatabaseError, err)
	}

	// Enable WAL mode for better concurrency
	if _, err := database.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("%w: failed to enable WAL mode: %w", ErrDatabaseError, err)
	}

	store := &Store{db: database}
	if err := store.Initialize(); err != nil {
		_ = database.Close()
		return nil, err
	}

	return store, nil
}

// Initialize creates the database schema.
func (s *Store) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(context.Background(), Schema)
	if err != nil {
		return fmt.Errorf("%w: failed to initialize schema: %w", ErrDatabaseError, err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ValidateBucketName checks if the bucket name is valid.
func ValidateBucketName(name string) error {
	if len(name) < bucketNameMinLength || len(name) > bucketNameMaxLength {
		return ErrInvalidBucketName
	}
	if !bucketNamePattern.MatchString(name) {
		return ErrInvalidBucketName
	}
	return nil
}

// CreateBucket creates a new bucket.
func (s *Store) CreateBucket(name, ownerID string, opts *BucketOptions) (*models.Bucket, error) {
	if err := ValidateBucketName(name); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	isPublic := false
	quotaBytes := int64(0)
	if opts != nil {
		isPublic = opts.IsPublic
		quotaBytes = opts.QuotaBytes
	}

	now := time.Now()
	result, err := s.db.ExecContext(context.Background(),
		`INSERT INTO buckets (name, owner_id, created_at, updated_at, is_public, quota_bytes) VALUES (?, ?, ?, ?, ?, ?)`,
		name, ownerID, now, now, isPublic, quotaBytes,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrBucketExists
		}
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	bucketID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return &models.Bucket{
		ID:         bucketID,
		Name:       name,
		OwnerID:    ownerID,
		CreatedAt:  now,
		UpdatedAt:  now,
		IsPublic:   isPublic,
		QuotaBytes: quotaBytes,
	}, nil
}

// GetBucket retrieves a bucket by name.
func (s *Store) GetBucket(name string) (*models.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()
	bucketRecord := &models.Bucket{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, created_at, updated_at, is_public, quota_bytes FROM buckets WHERE name = ?`,
		name,
	).Scan(&bucketRecord.ID, &bucketRecord.Name, &bucketRecord.OwnerID, &bucketRecord.CreatedAt, &bucketRecord.UpdatedAt, &bucketRecord.IsPublic, &bucketRecord.QuotaBytes)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	// Get object count and total size
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size), 0) FROM objects WHERE bucket_id = ?`,
		bucketRecord.ID,
	).Scan(&bucketRecord.ObjectCount, &bucketRecord.TotalSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return bucketRecord, nil
}

// GetBucketByID retrieves a bucket by ID.
func (s *Store) GetBucketByID(bucketID int64) (*models.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bucketRecord := &models.Bucket{}
	err := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, owner_id, created_at, updated_at, is_public, quota_bytes FROM buckets WHERE id = ?`,
		bucketID,
	).Scan(&bucketRecord.ID, &bucketRecord.Name, &bucketRecord.OwnerID, &bucketRecord.CreatedAt, &bucketRecord.UpdatedAt, &bucketRecord.IsPublic, &bucketRecord.QuotaBytes)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return bucketRecord, nil
}

// DeleteBucket deletes a bucket if it is empty.
func (s *Store) DeleteBucket(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	// Check if bucket exists and is empty
	var (
		bucketID    int64
		objectCount int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT id FROM buckets WHERE name = ?`, name).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBucketNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects WHERE bucket_id = ?`, bucketID).Scan(&objectCount)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	if objectCount > 0 {
		return ErrBucketNotEmpty
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM buckets WHERE id = ?`, bucketID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return nil
}

// ListBuckets lists all buckets for an owner.
func (s *Store) ListBuckets(ownerID string) ([]models.Bucket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(context.Background(),
		`SELECT b.id, b.name, b.owner_id, b.created_at, b.updated_at, b.is_public, b.quota_bytes,
		        COUNT(o.id), COALESCE(SUM(o.size), 0)
		 FROM buckets b
		 LEFT JOIN objects o ON b.id = o.bucket_id
		 WHERE b.owner_id = ?
		 GROUP BY b.id
		 ORDER BY b.name`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}
	defer func() { _ = rows.Close() }()

	var buckets []models.Bucket
	for rows.Next() {
		var bucketRecord models.Bucket
		err := rows.Scan(
			&bucketRecord.ID, &bucketRecord.Name, &bucketRecord.OwnerID, &bucketRecord.CreatedAt, &bucketRecord.UpdatedAt,
			&bucketRecord.IsPublic, &bucketRecord.QuotaBytes, &bucketRecord.ObjectCount, &bucketRecord.TotalSize,
		)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
		}
		buckets = append(buckets, bucketRecord)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return buckets, nil
}

// BucketExists checks if a bucket exists.
func (s *Store) BucketExists(name string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM buckets WHERE name = ?)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}
	return exists, nil
}

// PutObject creates or updates an object in a bucket.
func (s *Store) PutObject(bucketName, key, hash string, size int64, contentType string, metadata map[string]string) (*models.BucketObject, error) {
	if len(hash) != hashLength {
		return nil, fmt.Errorf("%w: invalid hash length", ErrDatabaseError)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	// Get bucket ID
	var bucketID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM buckets WHERE name = ?`, bucketName).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	// Serialize metadata
	var metadataJSON []byte
	if len(metadata) > 0 {
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to serialize metadata: %w", ErrDatabaseError, err)
		}
	}

	now := time.Now()

	// Use INSERT OR REPLACE to handle upsert
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO objects (bucket_id, key, hash, size, content_type, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(bucket_id, key) DO UPDATE SET
		 hash = excluded.hash,
		 size = excluded.size,
		 content_type = excluded.content_type,
		 metadata = excluded.metadata,
		 updated_at = excluded.updated_at`,
		bucketID, key, hash, size, contentType, string(metadataJSON), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	objectID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return &models.BucketObject{
		ID:          objectID,
		BucketID:    bucketID,
		Key:         key,
		Hash:        hash,
		Size:        size,
		ContentType: contentType,
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetObject retrieves an object by bucket name and key.
func (s *Store) GetObject(bucketName, key string) (*models.BucketObject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()

	var bucketID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM buckets WHERE name = ?`, bucketName).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	obj := &models.BucketObject{BucketID: bucketID}
	var (
		metadataJSON   sql.NullString
		objContentType sql.NullString
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT id, key, hash, size, content_type, metadata, created_at, updated_at
		 FROM objects WHERE bucket_id = ? AND key = ?`,
		bucketID, key,
	).Scan(&obj.ID, &obj.Key, &obj.Hash, &obj.Size, &objContentType, &metadataJSON, &obj.CreatedAt, &obj.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	if objContentType.Valid {
		obj.ContentType = objContentType.String
	}

	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &obj.Metadata); err != nil {
			return nil, fmt.Errorf("%w: failed to parse metadata: %w", ErrDatabaseError, err)
		}
	}

	return obj, nil
}

// DeleteObject removes an object from a bucket.
func (s *Store) DeleteObject(bucketName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()

	var bucketID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM buckets WHERE name = ?`, bucketName).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBucketNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM objects WHERE bucket_id = ? AND key = ?`, bucketID, key)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	if rowsAffected == 0 {
		return ErrObjectNotFound
	}

	return nil
}

// ListObjects lists objects in a bucket with optional prefix and pagination.
//
//nolint:cyclop,funlen // Complex but necessary logic for object listing with pagination
func (s *Store) ListObjects(bucketName string, opts *ListOptions) (*models.ObjectListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()

	var bucketID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM buckets WHERE name = ?`, bucketName).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	maxKeys := 1000
	if opts != nil && opts.MaxKeys > 0 && opts.MaxKeys < 1000 {
		maxKeys = opts.MaxKeys
	}

	// Build query
	query := `SELECT id, key, hash, size, content_type, metadata, created_at, updated_at
	          FROM objects WHERE bucket_id = ?`
	args := []interface{}{bucketID}

	if opts != nil && opts.Prefix != "" {
		query += ` AND key LIKE ?`
		args = append(args, opts.Prefix+"%")
	}

	if opts != nil && opts.Cursor != "" {
		cursorID, parseErr := strconv.ParseInt(opts.Cursor, 10, 64)
		if parseErr == nil {
			query += ` AND id > ?`
			args = append(args, cursorID)
		}
	}

	query += ` ORDER BY key LIMIT ?`
	args = append(args, maxKeys+1) // Fetch one extra to check if there are more

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		lastID  int64
		objects []models.BucketObject
	)
	for rows.Next() {
		var (
			metadataJSON   sql.NullString
			obj            models.BucketObject
			objContentType sql.NullString
		)
		obj.BucketID = bucketID

		scanErr := rows.Scan(&obj.ID, &obj.Key, &obj.Hash, &obj.Size, &objContentType, &metadataJSON, &obj.CreatedAt, &obj.UpdatedAt)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrDatabaseError, scanErr)
		}

		if objContentType.Valid {
			obj.ContentType = objContentType.String
		}

		if metadataJSON.Valid && metadataJSON.String != "" {
			if unmarshalErr := json.Unmarshal([]byte(metadataJSON.String), &obj.Metadata); unmarshalErr != nil {
				return nil, fmt.Errorf("%w: failed to parse metadata: %w", ErrDatabaseError, unmarshalErr)
			}
		}

		lastID = obj.ID
		objects = append(objects, obj)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	response := &models.ObjectListResponse{
		Objects: objects,
	}

	// Check if there are more results
	if len(objects) > maxKeys {
		response.Objects = objects[:maxKeys]
		response.IsTruncated = true
		response.NextCursor = strconv.FormatInt(lastID, 10)
	}

	if opts != nil {
		response.Prefix = opts.Prefix
		response.Delimiter = opts.Delimiter
	}

	// Handle delimiter for common prefixes (folder-like listing)
	if opts != nil && opts.Delimiter != "" {
		response.CommonPrefixes = s.extractCommonPrefixes(response.Objects, opts.Prefix, opts.Delimiter)
	}

	return response, nil
}

// extractCommonPrefixes extracts common prefixes from object keys for folder-like listing.
//
//nolint:funcorder // Placed near caller for readability
func (s *Store) extractCommonPrefixes(objects []models.BucketObject, prefix, delimiter string) []string {
	prefixSet := make(map[string]bool)

	for _, obj := range objects {
		keyPart := obj.Key
		if prefix != "" {
			keyPart = strings.TrimPrefix(keyPart, prefix)
		}

		if idx := strings.Index(keyPart, delimiter); idx >= 0 {
			commonPrefix := prefix + keyPart[:idx+len(delimiter)]
			prefixSet[commonPrefix] = true
		}
	}

	prefixes := make([]string, 0, len(prefixSet))
	for prefixVal := range prefixSet {
		prefixes = append(prefixes, prefixVal)
	}

	return prefixes
}

// GetHashReferences returns the bucket names that reference a given hash.
func (s *Store) GetHashReferences(hash string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(context.Background(),
		`SELECT DISTINCT b.name FROM buckets b
		 INNER JOIN objects o ON b.id = o.bucket_id
		 WHERE o.hash = ?`,
		hash,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}
	defer func() { _ = rows.Close() }()

	var bucketNames []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrDatabaseError, scanErr)
		}
		bucketNames = append(bucketNames, name)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	return bucketNames, nil
}

// IsHashReferenced checks if any bucket references the given hash.
func (s *Store) IsHashReferenced(hash string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM objects WHERE hash = ?)`, hash).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}
	return exists, nil
}

// CheckAccess verifies if a user has access to a bucket.
func (s *Store) CheckAccess(bucketName, userID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		isPublic bool
		ownerID  string
	)
	err := s.db.QueryRowContext(context.Background(), `SELECT owner_id, is_public FROM buckets WHERE name = ?`, bucketName).Scan(&ownerID, &isPublic)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBucketNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseError, err)
	}

	// Owner always has access
	if ownerID == userID {
		return nil
	}

	// Public buckets allow read access
	if isPublic {
		return nil
	}

	return ErrAccessDenied
}
