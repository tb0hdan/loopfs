package bucket

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// StoreTestSuite tests the bucket Store functionality.
type StoreTestSuite struct {
	suite.Suite
	tempDir  string
	dbPath   string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests.
func (s *StoreTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "bucket-store-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing.
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests.
func (s *StoreTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test.
func (s *StoreTestSuite) SetupTest() {
	s.dbPath = filepath.Join(s.tempDir, "test.db")
	var err error
	s.store, err = NewStore(s.dbPath)
	s.Require().NoError(err)
}

// TearDownTest runs after each test.
func (s *StoreTestSuite) TearDownTest() {
	if s.store != nil {
		s.store.Close()
	}
	os.Remove(s.dbPath)
}

// TestNewStore tests store creation.
func (s *StoreTestSuite) TestNewStore() {
	s.NotNil(s.store)
}

// TestNewStoreInvalidPath tests store creation with invalid path.
func (s *StoreTestSuite) TestNewStoreInvalidPath() {
	_, err := NewStore("/nonexistent/path/to/db.sqlite")
	s.Error(err)
}

// TestValidateBucketName tests bucket name validation.
func (s *StoreTestSuite) TestValidateBucketName() {
	testCases := []struct {
		name    string
		valid   bool
		message string
	}{
		{"my-bucket", true, "valid bucket name with hyphen"},
		{"mybucket", true, "valid simple bucket name"},
		{"abc", true, "valid minimum length bucket name"},
		{"a-b", true, "valid short bucket name with hyphen"},
		{"ab", false, "too short bucket name"},
		{"a", false, "single char bucket name"},
		{"", false, "empty bucket name"},
		{"-bucket", false, "starts with hyphen"},
		{"bucket-", false, "ends with hyphen"},
		{"BUCKET", false, "uppercase letters"},
		{"my_bucket", false, "underscore not allowed"},
		{"my.bucket", false, "dot not allowed"},
		{"my bucket", false, "space not allowed"},
	}

	for _, tc := range testCases {
		err := ValidateBucketName(tc.name)
		if tc.valid {
			s.NoError(err, tc.message)
		} else {
			s.Error(err, tc.message)
		}
	}
}

// TestCreateBucket tests bucket creation.
func (s *StoreTestSuite) TestCreateBucket() {
	bucket, err := s.store.CreateBucket("test-bucket", "owner1", nil)
	s.Require().NoError(err)
	s.NotNil(bucket)
	s.Equal("test-bucket", bucket.Name)
	s.Equal("owner1", bucket.OwnerID)
	s.False(bucket.IsPublic)
	s.Equal(int64(0), bucket.QuotaBytes)
}

// TestCreateBucketWithOptions tests bucket creation with options.
func (s *StoreTestSuite) TestCreateBucketWithOptions() {
	opts := &BucketOptions{
		IsPublic:   true,
		QuotaBytes: 1024 * 1024 * 100, // 100MB
	}
	bucket, err := s.store.CreateBucket("public-bucket", "owner1", opts)
	s.Require().NoError(err)
	s.NotNil(bucket)
	s.True(bucket.IsPublic)
	s.Equal(int64(1024*1024*100), bucket.QuotaBytes)
}

// TestCreateBucketDuplicate tests duplicate bucket creation.
func (s *StoreTestSuite) TestCreateBucketDuplicate() {
	_, err := s.store.CreateBucket("my-bucket", "owner1", nil)
	s.Require().NoError(err)

	_, err = s.store.CreateBucket("my-bucket", "owner2", nil)
	s.ErrorIs(err, ErrBucketExists)
}

// TestCreateBucketInvalidName tests bucket creation with invalid name.
func (s *StoreTestSuite) TestCreateBucketInvalidName() {
	_, err := s.store.CreateBucket("ab", "owner1", nil)
	s.ErrorIs(err, ErrInvalidBucketName)
}

// TestGetBucket tests bucket retrieval.
func (s *StoreTestSuite) TestGetBucket() {
	_, err := s.store.CreateBucket("get-bucket", "owner1", nil)
	s.Require().NoError(err)

	bucket, err := s.store.GetBucket("get-bucket")
	s.Require().NoError(err)
	s.Equal("get-bucket", bucket.Name)
	s.Equal("owner1", bucket.OwnerID)
	s.Equal(int64(0), bucket.ObjectCount)
	s.Equal(int64(0), bucket.TotalSize)
}

// TestGetBucketNotFound tests getting non-existent bucket.
func (s *StoreTestSuite) TestGetBucketNotFound() {
	_, err := s.store.GetBucket("nonexistent")
	s.ErrorIs(err, ErrBucketNotFound)
}

// TestDeleteBucket tests bucket deletion.
func (s *StoreTestSuite) TestDeleteBucket() {
	_, err := s.store.CreateBucket("delete-bucket", "owner1", nil)
	s.Require().NoError(err)

	err = s.store.DeleteBucket("delete-bucket")
	s.NoError(err)

	_, err = s.store.GetBucket("delete-bucket")
	s.ErrorIs(err, ErrBucketNotFound)
}

// TestDeleteBucketNotFound tests deleting non-existent bucket.
func (s *StoreTestSuite) TestDeleteBucketNotFound() {
	err := s.store.DeleteBucket("nonexistent")
	s.ErrorIs(err, ErrBucketNotFound)
}

// TestDeleteBucketNotEmpty tests deleting non-empty bucket.
func (s *StoreTestSuite) TestDeleteBucketNotEmpty() {
	_, err := s.store.CreateBucket("nonempty-bucket", "owner1", nil)
	s.Require().NoError(err)

	_, err = s.store.PutObject("nonempty-bucket", "file.txt", s.testHash, 100, "text/plain", nil)
	s.Require().NoError(err)

	err = s.store.DeleteBucket("nonempty-bucket")
	s.ErrorIs(err, ErrBucketNotEmpty)
}

// TestListBuckets tests listing buckets.
func (s *StoreTestSuite) TestListBuckets() {
	_, err := s.store.CreateBucket("bucket-a", "owner1", nil)
	s.Require().NoError(err)
	_, err = s.store.CreateBucket("bucket-b", "owner1", nil)
	s.Require().NoError(err)
	_, err = s.store.CreateBucket("bucket-c", "owner2", nil)
	s.Require().NoError(err)

	buckets, err := s.store.ListBuckets("owner1")
	s.Require().NoError(err)
	s.Len(buckets, 2)
	s.Equal("bucket-a", buckets[0].Name)
	s.Equal("bucket-b", buckets[1].Name)
}

// TestBucketExists tests bucket existence check.
func (s *StoreTestSuite) TestBucketExists() {
	_, err := s.store.CreateBucket("exists-bucket", "owner1", nil)
	s.Require().NoError(err)

	exists, err := s.store.BucketExists("exists-bucket")
	s.NoError(err)
	s.True(exists)

	exists, err = s.store.BucketExists("nonexistent")
	s.NoError(err)
	s.False(exists)
}

// TestPutObject tests object creation.
func (s *StoreTestSuite) TestPutObject() {
	_, err := s.store.CreateBucket("objects-bucket", "owner1", nil)
	s.Require().NoError(err)

	obj, err := s.store.PutObject("objects-bucket", "path/to/file.txt", s.testHash, 1024, "text/plain", nil)
	s.Require().NoError(err)
	s.NotNil(obj)
	s.Equal("path/to/file.txt", obj.Key)
	s.Equal(s.testHash, obj.Hash)
	s.Equal(int64(1024), obj.Size)
	s.Equal("text/plain", obj.ContentType)
}

// TestPutObjectWithMetadata tests object creation with metadata.
func (s *StoreTestSuite) TestPutObjectWithMetadata() {
	_, err := s.store.CreateBucket("meta-bucket", "owner1", nil)
	s.Require().NoError(err)

	metadata := map[string]string{
		"author":  "test-user",
		"version": "1.0",
	}
	obj, err := s.store.PutObject("meta-bucket", "file.txt", s.testHash, 100, "", metadata)
	s.Require().NoError(err)
	s.Equal("test-user", obj.Metadata["author"])
	s.Equal("1.0", obj.Metadata["version"])
}

// TestPutObjectUpdate tests object update (upsert).
func (s *StoreTestSuite) TestPutObjectUpdate() {
	_, err := s.store.CreateBucket("update-bucket", "owner1", nil)
	s.Require().NoError(err)

	newHash := "b1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"

	_, err = s.store.PutObject("update-bucket", "file.txt", s.testHash, 100, "text/plain", nil)
	s.Require().NoError(err)

	obj, err := s.store.PutObject("update-bucket", "file.txt", newHash, 200, "application/json", nil)
	s.Require().NoError(err)
	s.Equal(newHash, obj.Hash)
	s.Equal(int64(200), obj.Size)
	s.Equal("application/json", obj.ContentType)
}

// TestPutObjectBucketNotFound tests object creation in non-existent bucket.
func (s *StoreTestSuite) TestPutObjectBucketNotFound() {
	_, err := s.store.PutObject("nonexistent", "file.txt", s.testHash, 100, "", nil)
	s.ErrorIs(err, ErrBucketNotFound)
}

// TestPutObjectInvalidHash tests object creation with invalid hash.
func (s *StoreTestSuite) TestPutObjectInvalidHash() {
	_, err := s.store.CreateBucket("hash-bucket", "owner1", nil)
	s.Require().NoError(err)

	_, err = s.store.PutObject("hash-bucket", "file.txt", "invalid", 100, "", nil)
	s.Error(err)
}

// TestGetObject tests object retrieval.
func (s *StoreTestSuite) TestGetObject() {
	_, err := s.store.CreateBucket("get-object-bucket", "owner1", nil)
	s.Require().NoError(err)

	metadata := map[string]string{"key": "value"}
	_, err = s.store.PutObject("get-object-bucket", "myfile.txt", s.testHash, 512, "text/plain", metadata)
	s.Require().NoError(err)

	obj, err := s.store.GetObject("get-object-bucket", "myfile.txt")
	s.Require().NoError(err)
	s.Equal("myfile.txt", obj.Key)
	s.Equal(s.testHash, obj.Hash)
	s.Equal(int64(512), obj.Size)
	s.Equal("text/plain", obj.ContentType)
	s.Equal("value", obj.Metadata["key"])
}

// TestGetObjectNotFound tests getting non-existent object.
func (s *StoreTestSuite) TestGetObjectNotFound() {
	_, err := s.store.CreateBucket("empty-bucket", "owner1", nil)
	s.Require().NoError(err)

	_, err = s.store.GetObject("empty-bucket", "nonexistent.txt")
	s.ErrorIs(err, ErrObjectNotFound)
}

// TestGetObjectBucketNotFound tests getting object from non-existent bucket.
func (s *StoreTestSuite) TestGetObjectBucketNotFound() {
	_, err := s.store.GetObject("nonexistent", "file.txt")
	s.ErrorIs(err, ErrBucketNotFound)
}

// TestDeleteObject tests object deletion.
func (s *StoreTestSuite) TestDeleteObject() {
	_, err := s.store.CreateBucket("del-object-bucket", "owner1", nil)
	s.Require().NoError(err)

	_, err = s.store.PutObject("del-object-bucket", "file.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)

	err = s.store.DeleteObject("del-object-bucket", "file.txt")
	s.NoError(err)

	_, err = s.store.GetObject("del-object-bucket", "file.txt")
	s.ErrorIs(err, ErrObjectNotFound)
}

// TestDeleteObjectNotFound tests deleting non-existent object.
func (s *StoreTestSuite) TestDeleteObjectNotFound() {
	_, err := s.store.CreateBucket("del-empty-bucket", "owner1", nil)
	s.Require().NoError(err)

	err = s.store.DeleteObject("del-empty-bucket", "nonexistent.txt")
	s.ErrorIs(err, ErrObjectNotFound)
}

// TestListObjects tests object listing.
func (s *StoreTestSuite) TestListObjects() {
	_, err := s.store.CreateBucket("list-bucket", "owner1", nil)
	s.Require().NoError(err)

	hash2 := "b1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	hash3 := "c1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"

	_, err = s.store.PutObject("list-bucket", "a.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("list-bucket", "b.txt", hash2, 200, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("list-bucket", "c.txt", hash3, 300, "", nil)
	s.Require().NoError(err)

	result, err := s.store.ListObjects("list-bucket", nil)
	s.Require().NoError(err)
	s.Len(result.Objects, 3)
	s.False(result.IsTruncated)
}

// TestListObjectsWithPrefix tests object listing with prefix.
func (s *StoreTestSuite) TestListObjectsWithPrefix() {
	_, err := s.store.CreateBucket("prefix-bucket", "owner1", nil)
	s.Require().NoError(err)

	hash2 := "b1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	hash3 := "c1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"

	_, err = s.store.PutObject("prefix-bucket", "photos/a.jpg", s.testHash, 100, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("prefix-bucket", "photos/b.jpg", hash2, 200, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("prefix-bucket", "docs/readme.md", hash3, 300, "", nil)
	s.Require().NoError(err)

	result, err := s.store.ListObjects("prefix-bucket", &ListOptions{Prefix: "photos/"})
	s.Require().NoError(err)
	s.Len(result.Objects, 2)
	s.Equal("photos/", result.Prefix)
}

// TestListObjectsPagination tests object listing with pagination.
func (s *StoreTestSuite) TestListObjectsPagination() {
	_, err := s.store.CreateBucket("page-bucket", "owner1", nil)
	s.Require().NoError(err)

	// Create 5 objects with valid 64-character hashes
	hashes := []string{
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef1",
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef2",
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef3",
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef4",
	}
	for i := 0; i < 5; i++ {
		_, err = s.store.PutObject("page-bucket", "file"+string(rune('a'+i))+".txt", hashes[i], int64(100+i), "", nil)
		s.Require().NoError(err)
	}

	// Get first 2
	result, err := s.store.ListObjects("page-bucket", &ListOptions{MaxKeys: 2})
	s.Require().NoError(err)
	s.Len(result.Objects, 2)
	s.True(result.IsTruncated)
	s.NotEmpty(result.NextCursor)

	// Get next page
	result2, err := s.store.ListObjects("page-bucket", &ListOptions{MaxKeys: 2, Cursor: result.NextCursor})
	s.Require().NoError(err)
	s.Len(result2.Objects, 2)
}

// TestGetHashReferences tests hash reference lookup.
func (s *StoreTestSuite) TestGetHashReferences() {
	_, err := s.store.CreateBucket("ref-bucket-a", "owner1", nil)
	s.Require().NoError(err)
	_, err = s.store.CreateBucket("ref-bucket-b", "owner1", nil)
	s.Require().NoError(err)

	// Same hash in multiple buckets
	_, err = s.store.PutObject("ref-bucket-a", "file.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("ref-bucket-b", "file.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)

	refs, err := s.store.GetHashReferences(s.testHash)
	s.Require().NoError(err)
	s.Len(refs, 2)
}

// TestIsHashReferenced tests hash reference check.
func (s *StoreTestSuite) TestIsHashReferenced() {
	_, err := s.store.CreateBucket("check-ref-bucket", "owner1", nil)
	s.Require().NoError(err)

	// Before adding object
	referenced, err := s.store.IsHashReferenced(s.testHash)
	s.NoError(err)
	s.False(referenced)

	// After adding object
	_, err = s.store.PutObject("check-ref-bucket", "file.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)

	referenced, err = s.store.IsHashReferenced(s.testHash)
	s.NoError(err)
	s.True(referenced)
}

// TestCheckAccess tests access control.
func (s *StoreTestSuite) TestCheckAccess() {
	_, err := s.store.CreateBucket("access-bucket", "owner1", nil)
	s.Require().NoError(err)

	// Owner has access
	err = s.store.CheckAccess("access-bucket", "owner1")
	s.NoError(err)

	// Non-owner denied
	err = s.store.CheckAccess("access-bucket", "other-user")
	s.ErrorIs(err, ErrAccessDenied)
}

// TestCheckAccessPublicBucket tests access control for public bucket.
func (s *StoreTestSuite) TestCheckAccessPublicBucket() {
	_, err := s.store.CreateBucket("public-access-bucket", "owner1", &BucketOptions{IsPublic: true})
	s.Require().NoError(err)

	// Non-owner has access to public bucket
	err = s.store.CheckAccess("public-access-bucket", "other-user")
	s.NoError(err)
}

// TestBucketStatsAfterObjects tests bucket stats are updated after adding objects.
func (s *StoreTestSuite) TestBucketStatsAfterObjects() {
	_, err := s.store.CreateBucket("stats-bucket", "owner1", nil)
	s.Require().NoError(err)

	hash2 := "b1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"

	_, err = s.store.PutObject("stats-bucket", "file1.txt", s.testHash, 100, "", nil)
	s.Require().NoError(err)
	_, err = s.store.PutObject("stats-bucket", "file2.txt", hash2, 200, "", nil)
	s.Require().NoError(err)

	bucket, err := s.store.GetBucket("stats-bucket")
	s.Require().NoError(err)
	s.Equal(int64(2), bucket.ObjectCount)
	s.Equal(int64(300), bucket.TotalSize)
}

// TestSuite runs the test suite.
func TestSuite(t *testing.T) {
	suite.Run(t, new(StoreTestSuite))
}
