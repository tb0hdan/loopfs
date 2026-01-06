package balancer

import (
	"context"
	"errors"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"os/signal"
	"syscall"
	"time"

	"loopfs/pkg/bucket"
	"loopfs/pkg/log"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Server struct {
	backendURLs             []string
	retryMax                int
	gracefulShutdownTimeout time.Duration
	retryWaitMin            time.Duration
	retryWaitMax            time.Duration
	requestTimeout          time.Duration
	healthCheckInterval     time.Duration
	healthCheckTimeout      time.Duration
	echo                    *echo.Echo
	backendManager          *BackendManager
	bucketStore             *bucket.Store
	debug                   bool
	debugAddr               string
	dbPath                  string
}

func NewBalancerServer(
	backendURLs []string,
	retryMax int,
	gracefulShutdownTimeout, retryWaitMin, retryWaitMax, requestTimeout time.Duration,
	healthCheckInterval, healthCheckTimeout time.Duration,
	debug bool, debugAddr, dbPath string,
) *Server {
	return &Server{
		backendURLs:             backendURLs,
		retryMax:                retryMax,
		gracefulShutdownTimeout: gracefulShutdownTimeout,
		retryWaitMin:            retryWaitMin,
		retryWaitMax:            retryWaitMax,
		requestTimeout:          requestTimeout,
		healthCheckInterval:     healthCheckInterval,
		healthCheckTimeout:      healthCheckTimeout,
		echo:                    echo.New(),
		debug:                   debug,
		debugAddr:               debugAddr,
		dbPath:                  dbPath,
	}
}

func (b *Server) Start(addr string) error {
	// Create backend manager and start health checks
	b.backendManager = NewBackendManager(b.backendURLs, b.healthCheckInterval, b.healthCheckTimeout)
	b.backendManager.Start()

	// Initialize bucket store if database path is provided
	if b.dbPath != "" {
		var err error
		b.bucketStore, err = bucket.NewStore(b.dbPath)
		if err != nil {
			return err
		}
		log.Info().Str("db_path", b.dbPath).Msg("Bucket store initialized")
	}

	// Create casBalancer
	casBalancer := NewBalancer(b.backendManager, b.retryMax, b.retryWaitMin, b.retryWaitMax, b.requestTimeout)
	b.setupRoutes(casBalancer)

	// Start pprof server if in debug mode
	if b.debug {
		go func() {
			log.Info().Msgf("Starting pprof server on %s", b.debugAddr)
			log.Info().Msgf("%+v", http.ListenAndServe(b.debugAddr, nil)) //nolint:gosec
		}()
	}
	// Start server
	go func() {
		log.Info().Str("addr", addr).Msg("Starting CAS load balancer")
		if err := b.echo.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("Failed to start server")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	return b.Shutdown()
}

func (b *Server) Shutdown() error {
	log.Info().Msg("Shutting down server...")

	// Stop backend manager
	if b.backendManager != nil {
		b.backendManager.Stop()
	}

	// Close bucket store
	if b.bucketStore != nil {
		if err := b.bucketStore.Close(); err != nil {
			log.Warn().Err(err).Msg("Failed to close bucket store")
		}
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), b.gracefulShutdownTimeout)
	defer cancel()

	return b.echo.Shutdown(ctx)
}

// BackendManager returns the backend manager for this server.
func (b *Server) BackendManager() *BackendManager {
	return b.backendManager
}

func (b *Server) setupRoutes(casBalancer *Balancer) {
	// Create Echo server
	b.echo.HideBanner = true
	b.echo.HidePort = true

	// Add middleware
	b.echo.Use(middleware.Logger())
	b.echo.Use(middleware.Recover())
	b.echo.Use(middleware.CORS())

	// Register CAS routes (unchanged for backward compatibility)
	b.echo.POST("/file/upload", casBalancer.UploadHandler)
	b.echo.GET("/file/:hash/download", casBalancer.DownloadHandler)
	b.echo.GET("/file/:hash/info", casBalancer.FileInfoHandler)
	b.echo.DELETE("/file/:hash/delete", casBalancer.DeleteHandler)

	// Backend status endpoint
	b.echo.GET("/backends/status", func(ctx echo.Context) error {
		statuses := b.backendManager.GetAllBackendStatus()
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"backends": statuses,
			"online":   b.backendManager.HasOnlineBackends(),
		})
	})

	// Register bucket routes (only if bucket store is configured)
	if b.bucketStore != nil {
		bucketHandlers := NewBucketHandlers(b.bucketStore)
		objectHandlers := NewObjectHandlers(b.bucketStore, casBalancer, b.requestTimeout)

		// Bucket management
		b.echo.POST("/bucket/:name", bucketHandlers.CreateBucketHandler)
		b.echo.GET("/bucket/:name", bucketHandlers.GetBucketHandler)
		b.echo.DELETE("/bucket/:name", bucketHandlers.DeleteBucketHandler)
		b.echo.GET("/buckets", bucketHandlers.ListBucketsHandler)

		// Object operations
		b.echo.POST("/bucket/:name/upload", objectHandlers.BucketUploadHandler)
		b.echo.PUT("/bucket/:name/object/*", objectHandlers.PutObjectHandler)
		b.echo.GET("/bucket/:name/object/*", objectHandlers.GetObjectHandler)
		b.echo.HEAD("/bucket/:name/object/*", objectHandlers.HeadObjectHandler)
		b.echo.DELETE("/bucket/:name/object/*", objectHandlers.DeleteObjectHandler)
		b.echo.GET("/bucket/:name/objects", objectHandlers.ListObjectsHandler)

		log.Info().Msg("Bucket API routes enabled")
	}
}
