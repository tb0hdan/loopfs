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
	debug                   bool
	debugAddr               string
}

func NewBalancerServer(
	backendURLs []string,
	retryMax int,
	gracefulShutdownTimeout, retryWaitMin, retryWaitMax, requestTimeout time.Duration,
	healthCheckInterval, healthCheckTimeout time.Duration,
	debug bool, debugAddr string,
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
	}
}

func (b *Server) Start(addr string) error {
	// Create backend manager and start health checks
	b.backendManager = NewBackendManager(b.backendURLs, b.healthCheckInterval, b.healthCheckTimeout)
	b.backendManager.Start()

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

	// Register routes
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
}
