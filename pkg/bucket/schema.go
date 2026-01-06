package bucket

// Schema contains the SQL statements to create the bucket database schema.
const Schema = `
-- Buckets table: stores bucket metadata
CREATE TABLE IF NOT EXISTS buckets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT UNIQUE NOT NULL,
    owner_id    TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_public   BOOLEAN DEFAULT FALSE,
    quota_bytes INTEGER DEFAULT 0
);

-- Objects table: maps names to CAS hashes within buckets
CREATE TABLE IF NOT EXISTS objects (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_id    INTEGER NOT NULL,
    key          TEXT NOT NULL,
    hash         TEXT NOT NULL,
    size         INTEGER NOT NULL,
    content_type TEXT,
    metadata     TEXT,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (bucket_id) REFERENCES buckets(id) ON DELETE CASCADE,
    UNIQUE (bucket_id, key)
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_buckets_owner ON buckets(owner_id);
CREATE INDEX IF NOT EXISTS idx_buckets_name ON buckets(name);
CREATE INDEX IF NOT EXISTS idx_objects_bucket ON objects(bucket_id);
CREATE INDEX IF NOT EXISTS idx_objects_hash ON objects(hash);
CREATE INDEX IF NOT EXISTS idx_objects_key ON objects(bucket_id, key);
`

// bucketNameMinLength is the minimum length for a bucket name.
const bucketNameMinLength = 3

// bucketNameMaxLength is the maximum length for a bucket name.
const bucketNameMaxLength = 63

// hashLength is the expected length of a SHA256 hash in hexadecimal format.
const hashLength = 64
