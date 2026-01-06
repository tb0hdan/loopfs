package bucket

import "errors"

var (
	// ErrBucketExists is returned when attempting to create a bucket that already exists.
	ErrBucketExists = errors.New("bucket already exists")

	// ErrBucketNotFound is returned when the requested bucket does not exist.
	ErrBucketNotFound = errors.New("bucket not found")

	// ErrBucketNotEmpty is returned when attempting to delete a bucket that contains objects.
	ErrBucketNotEmpty = errors.New("bucket is not empty")

	// ErrObjectNotFound is returned when the requested object does not exist.
	ErrObjectNotFound = errors.New("object not found")

	// ErrObjectExists is returned when attempting to create an object that already exists.
	ErrObjectExists = errors.New("object already exists")

	// ErrInvalidBucketName is returned when the bucket name does not meet naming requirements.
	ErrInvalidBucketName = errors.New("invalid bucket name")

	// ErrQuotaExceeded is returned when the bucket quota would be exceeded.
	ErrQuotaExceeded = errors.New("bucket quota exceeded")

	// ErrAccessDenied is returned when the user does not have permission to perform the operation.
	ErrAccessDenied = errors.New("access denied")

	// ErrDatabaseError is returned when a database operation fails.
	ErrDatabaseError = errors.New("database error")
)
