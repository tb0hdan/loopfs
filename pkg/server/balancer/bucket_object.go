package balancer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"loopfs/pkg/bucket"
	"loopfs/pkg/log"
	"loopfs/pkg/models"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

// ObjectHandlers contains handlers for bucket object operations.
type ObjectHandlers struct {
	bucketStore    *bucket.Store
	balancer       *Balancer
	requestTimeout time.Duration
}

// NewObjectHandlers creates a new ObjectHandlers instance.
func NewObjectHandlers(bucketStore *bucket.Store, balancer *Balancer, requestTimeout time.Duration) *ObjectHandlers {
	return &ObjectHandlers{
		bucketStore:    bucketStore,
		balancer:       balancer,
		requestTimeout: requestTimeout,
	}
}

// BucketUploadHandler handles file upload to a bucket.
// POST /bucket/:name/upload.
func (h *ObjectHandlers) BucketUploadHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	ownerID := getOwnerFromContext(ctx)

	// Verify bucket exists and user has write access
	bucketRecord, err := h.bucketStore.GetBucket(bucketName)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to get bucket",
		})
	}

	// Check write access (only owner can write)
	if bucketRecord.OwnerID != ownerID {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "Access denied",
		})
	}

	// Parse multipart form
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "No file provided",
		})
	}

	// Get object key from form or use filename
	key := ctx.FormValue("key")
	if key == "" {
		key = file.Filename
	}

	// Get content type
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Perform CAS upload
	hash, size, err := h.performCASUpload(ctx, file)
	if err != nil {
		log.Error().Err(err).Str("bucket", bucketName).Str("key", key).Msg("CAS upload failed")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Upload failed: " + err.Error(),
		})
	}

	// Create object record in bucket
	obj, err := h.bucketStore.PutObject(bucketName, key, hash, size, contentType, nil)
	if err != nil {
		log.Error().Err(err).Str("bucket", bucketName).Str("key", key).Msg("Failed to create object record")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to create object record",
		})
	}

	return ctx.JSON(http.StatusOK, models.BucketUploadResponse{
		Hash:   obj.Hash,
		Key:    obj.Key,
		Bucket: bucketName,
		Size:   obj.Size,
	})
}

// performCASUpload forwards the file upload to a CAS backend and returns the hash.
//
//nolint:cyclop,funcorder // Complex but necessary logic for CAS upload; placed near caller for readability
func (h *ObjectHandlers) performCASUpload(ctx echo.Context, file *multipart.FileHeader) (string, int64, error) {
	// Check if any backends are online
	if !h.balancer.backendManager.HasOnlineBackends() {
		return "", 0, ErrAllBackendsDown
	}

	fileSize := file.Size

	// Select backend with most available space
	backend, err := h.balancer.backendManager.GetBackendForUpload(fileSize)
	if err != nil {
		return "", 0, err
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), h.requestTimeout)
	defer cancel()

	// Prepare streaming multipart request (reuse function from upload.go)
	boundary := fmt.Sprintf("loopfs-%d", time.Now().UnixNano())
	uploadBody, contentType, err := createStreamingBody(reqCtx, file, boundary)
	if err != nil {
		return "", 0, fmt.Errorf("failed to prepare upload body: %w", err)
	}
	defer func() {
		if closeErr := uploadBody.Close(); closeErr != nil && !errors.Is(closeErr, io.ErrClosedPipe) {
			log.Warn().Err(closeErr).Msg("Failed to close upload body")
		}
	}()

	// Create request
	req, err := retryablehttp.NewRequestWithContext(reqCtx, "POST", backend+"/file/upload", uploadBody)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	// Execute request
	resp, err := h.balancer.client.Do(req)
	if err != nil {
		if isTimeoutOrConnectionError(err) {
			h.balancer.backendManager.MarkBackendDead(backend, err)
		}
		return "", 0, fmt.Errorf("upload request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close upload response body")
		}
	}()

	// Parse response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return "", 0, fmt.Errorf("backend returned status %d", resp.StatusCode)
	}

	var uploadResp struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return uploadResp.Hash, fileSize, nil
}


// GetObjectHandler handles object download by key.
// GET /bucket/:name/object/*.
func (h *ObjectHandlers) GetObjectHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	key := ctx.Param("*")
	ownerID := getOwnerFromContext(ctx)

	// Look up object to get hash
	obj, err := h.bucketStore.GetObject(bucketName, key)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		if errors.Is(err, bucket.ErrObjectNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Object not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to get object",
		})
	}

	// Check access
	if err := h.bucketStore.CheckAccess(bucketName, ownerID); err != nil {
		if errors.Is(err, bucket.ErrAccessDenied) {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "Access denied",
			})
		}
	}

	// Download from CAS using existing download logic
	return h.downloadByHash(ctx, obj.Hash, obj.ContentType)
}

// downloadByHash performs the CAS download using the hash.
//
//nolint:cyclop,bodyclose,funcorder // Complex but necessary logic for parallel backend requests; body is closed in for loop; placed near caller for readability
func (h *ObjectHandlers) downloadByHash(ctx echo.Context, hash, contentType string) error {
	backendURLs := h.balancer.backendManager.GetOnlineBackends()
	if len(backendURLs) == 0 {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": ErrAllBackendsDown.Error(),
		})
	}

	// Execute parallel download requests
	results := executeBackendRequests(
		ctx.Request().Context(),
		backendURLs,
		h.requestTimeout,
		func(reqCtx context.Context, backend string) (*http.Response, int, error) {
			req, err := retryablehttp.NewRequestWithContext(reqCtx, "GET", backend+"/file/"+hash+"/download", nil)
			if err != nil {
				return nil, 0, err
			}

			resp, err := h.balancer.client.Do(req)
			if err != nil {
				return nil, 0, err
			}

			return resp, resp.StatusCode, nil
		},
		true, // Cancel other requests on success
	)

	// Wait for first successful response
	for result := range results {
		if result.Error == nil && result.Status == http.StatusOK {
			resp := result.Data
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Msg("Failed to close download response body")
				}
			}()

			// Set content type if known
			if contentType != "" {
				ctx.Response().Header().Set(echo.HeaderContentType, contentType)
			} else if ct := resp.Header.Get(echo.HeaderContentType); ct != "" {
				ctx.Response().Header().Set(echo.HeaderContentType, ct)
			}

			// Stream response
			ctx.Response().WriteHeader(http.StatusOK)
			_, err := io.Copy(ctx.Response(), resp.Body)
			if err != nil {
				log.Warn().Err(err).Msg("Error streaming download response")
			}
			return nil
		}

		// Clean up failed response
		if result.Data != nil {
			_ = result.Data.Body.Close()
		}
	}

	return ctx.JSON(http.StatusNotFound, map[string]string{
		"error": "Object not found in storage",
	})
}

// PutObjectHandler handles object upload at a specific key.
// PUT /bucket/:name/object/*.
func (h *ObjectHandlers) PutObjectHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	key := ctx.Param("*")
	ownerID := getOwnerFromContext(ctx)

	// Verify bucket exists and user has write access
	bucketRecord, err := h.bucketStore.GetBucket(bucketName)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to get bucket",
		})
	}

	if bucketRecord.OwnerID != ownerID {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "Access denied",
		})
	}

	// Parse multipart form
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "No file provided",
		})
	}

	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Perform CAS upload
	hash, size, err := h.performCASUpload(ctx, file)
	if err != nil {
		log.Error().Err(err).Str("bucket", bucketName).Str("key", key).Msg("CAS upload failed")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Upload failed: " + err.Error(),
		})
	}

	// Create/update object record
	obj, err := h.bucketStore.PutObject(bucketName, key, hash, size, contentType, nil)
	if err != nil {
		log.Error().Err(err).Str("bucket", bucketName).Str("key", key).Msg("Failed to create object record")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to create object record",
		})
	}

	return ctx.JSON(http.StatusOK, models.BucketUploadResponse{
		Hash:   obj.Hash,
		Key:    obj.Key,
		Bucket: bucketName,
		Size:   obj.Size,
	})
}

// HeadObjectHandler returns object metadata without body.
// HEAD /bucket/:name/object/*.
func (h *ObjectHandlers) HeadObjectHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	key := ctx.Param("*")
	ownerID := getOwnerFromContext(ctx)

	obj, err := h.bucketStore.GetObject(bucketName, key)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.NoContent(http.StatusNotFound)
		}
		if errors.Is(err, bucket.ErrObjectNotFound) {
			return ctx.NoContent(http.StatusNotFound)
		}
		return ctx.NoContent(http.StatusInternalServerError)
	}

	// Check access
	if err := h.bucketStore.CheckAccess(bucketName, ownerID); err != nil {
		if errors.Is(err, bucket.ErrAccessDenied) {
			return ctx.NoContent(http.StatusForbidden)
		}
	}

	// Set headers
	ctx.Response().Header().Set("X-Object-Hash", obj.Hash)
	ctx.Response().Header().Set("X-Object-Size", strconv.FormatInt(obj.Size, 10))
	ctx.Response().Header().Set("X-Object-Key", obj.Key)
	ctx.Response().Header().Set("Last-Modified", obj.UpdatedAt.Format(http.TimeFormat))
	if obj.ContentType != "" {
		ctx.Response().Header().Set(echo.HeaderContentType, obj.ContentType)
	}

	return ctx.NoContent(http.StatusOK)
}

// DeleteObjectHandler removes an object reference.
// DELETE /bucket/:name/object/*.
func (h *ObjectHandlers) DeleteObjectHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	key := ctx.Param("*")
	ownerID := getOwnerFromContext(ctx)

	// Verify ownership
	bucketRecord, err := h.bucketStore.GetBucket(bucketName)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to get bucket",
		})
	}

	if bucketRecord.OwnerID != ownerID {
		return ctx.JSON(http.StatusForbidden, map[string]string{
			"error": "Access denied",
		})
	}

	err = h.bucketStore.DeleteObject(bucketName, key)
	if err != nil {
		if errors.Is(err, bucket.ErrObjectNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Object not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to delete object",
		})
	}

	// Note: We don't delete from CAS - deduplication means other buckets may reference the same hash.
	// Garbage collection of orphaned CAS content is a separate process.

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "Object deleted successfully",
		"bucket":  bucketName,
		"key":     key,
	})
}

// ListObjectsHandler lists objects in a bucket with optional prefix.
// GET /bucket/:name/objects.
func (h *ObjectHandlers) ListObjectsHandler(ctx echo.Context) error {
	bucketName := ctx.Param("name")
	ownerID := getOwnerFromContext(ctx)

	// Check access
	if err := h.bucketStore.CheckAccess(bucketName, ownerID); err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		if errors.Is(err, bucket.ErrAccessDenied) {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "Access denied",
			})
		}
	}

	// Parse list options
	opts := &bucket.ListOptions{
		Prefix:    ctx.QueryParam("prefix"),
		Delimiter: ctx.QueryParam("delimiter"),
		Cursor:    ctx.QueryParam("cursor"),
	}

	if maxKeysStr := ctx.QueryParam("max-keys"); maxKeysStr != "" {
		if maxKeys, err := strconv.Atoi(maxKeysStr); err == nil && maxKeys > 0 {
			opts.MaxKeys = maxKeys
		}
	}

	result, err := h.bucketStore.ListObjects(bucketName, opts)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "Bucket not found",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to list objects",
		})
	}

	// Ensure objects is not nil
	if result.Objects == nil {
		result.Objects = []models.BucketObject{}
	}

	return ctx.JSON(http.StatusOK, result)
}

