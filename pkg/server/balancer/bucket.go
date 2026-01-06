package balancer

import (
	"errors"
	"net/http"

	"loopfs/pkg/bucket"
	"loopfs/pkg/models"

	"github.com/labstack/echo/v4"
)

// BucketHandlers contains handlers for bucket management operations.
type BucketHandlers struct {
	store *bucket.Store
}

// NewBucketHandlers creates a new BucketHandlers instance.
func NewBucketHandlers(store *bucket.Store) *BucketHandlers {
	return &BucketHandlers{store: store}
}

// getOwnerFromContext extracts the owner ID from the request context.
// For now, uses X-Owner-ID header. In production, this should use proper authentication.
func getOwnerFromContext(ctx echo.Context) string {
	ownerID := ctx.Request().Header.Get("X-Owner-ID")
	if ownerID == "" {
		return "default"
	}
	return ownerID
}

// CreateBucketHandler handles bucket creation requests.
// POST /bucket/:name.
func (h *BucketHandlers) CreateBucketHandler(ctx echo.Context) error {
	name := ctx.Param("name")
	ownerID := getOwnerFromContext(ctx)

	// Parse optional options from request body
	var opts bucket.BucketOptions
	if err := ctx.Bind(&opts); err != nil {
		// Ignore binding errors - use defaults
		opts = bucket.BucketOptions{}
	}

	bucketRecord, err := h.store.CreateBucket(name, ownerID, &opts)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketExists) {
			return ctx.JSON(http.StatusConflict, map[string]string{
				"error": "Bucket already exists",
			})
		}
		if errors.Is(err, bucket.ErrInvalidBucketName) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "Invalid bucket name. Must be 3-63 characters, lowercase alphanumeric with hyphens.",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to create bucket",
		})
	}

	return ctx.JSON(http.StatusCreated, bucketRecord)
}

// GetBucketHandler handles bucket info requests.
// GET /bucket/:name.
func (h *BucketHandlers) GetBucketHandler(ctx echo.Context) error {
	name := ctx.Param("name")
	ownerID := getOwnerFromContext(ctx)

	bucketRecord, err := h.store.GetBucket(name)
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

	// Check access (read access for public buckets or owner)
	if err := h.store.CheckAccess(name, ownerID); err != nil {
		if errors.Is(err, bucket.ErrAccessDenied) {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "Access denied",
			})
		}
	}

	return ctx.JSON(http.StatusOK, bucketRecord)
}

// DeleteBucketHandler handles bucket deletion requests.
// DELETE /bucket/:name.
func (h *BucketHandlers) DeleteBucketHandler(ctx echo.Context) error {
	name := ctx.Param("name")
	ownerID := getOwnerFromContext(ctx)

	// Verify ownership
	bucketRecord, err := h.store.GetBucket(name)
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
			"error": "Only the bucket owner can delete the bucket",
		})
	}

	err = h.store.DeleteBucket(name)
	if err != nil {
		if errors.Is(err, bucket.ErrBucketNotEmpty) {
			return ctx.JSON(http.StatusConflict, map[string]string{
				"error": "Bucket is not empty. Delete all objects first.",
			})
		}
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to delete bucket",
		})
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "Bucket deleted successfully",
		"bucket":  name,
	})
}

// ListBucketsHandler handles listing buckets for the authenticated user.
// GET /buckets.
func (h *BucketHandlers) ListBucketsHandler(ctx echo.Context) error {
	ownerID := getOwnerFromContext(ctx)

	buckets, err := h.store.ListBuckets(ownerID)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to list buckets",
		})
	}

	if buckets == nil {
		buckets = []models.Bucket{}
	}

	return ctx.JSON(http.StatusOK, models.BucketListResponse{
		Buckets: buckets,
	})
}
