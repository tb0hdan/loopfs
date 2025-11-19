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
	backendList             []string
	retryMax                int
	gracefulShutdownTimeout time.Duration
	retryWaitMin            time.Duration
	retryWaitMax            time.Duration
	requestTimeout          time.Duration
	echo                    *echo.Echo
	debug                   bool
	debugAddr               string
}

func NewBalancerServer(backendList []string, retryMax int, gracefulShutdownTimeout, retryWaitMin, retryWaitMax,
	requestTimeout time.Duration, debug bool, debugAddr string) *Server {
	return &Server{
		backendList:             backendList,
		retryMax:                retryMax,
		gracefulShutdownTimeout: gracefulShutdownTimeout,
		retryWaitMin:            retryWaitMin,
		retryWaitMax:            retryWaitMax,
		requestTimeout:          requestTimeout,
		echo:                    echo.New(),
		debug:                   debug,
		debugAddr:               debugAddr,
	}
}

func (b *Server) Start(addr string) error {
	// Create casBalancer
	casBalancer := NewBalancer(b.backendList, b.retryMax, b.retryWaitMin, b.retryWaitMax, b.requestTimeout)
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
		log.Info().Str("addr", addr).Msg("Starting CAS load casBalancer")
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

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), b.gracefulShutdownTimeout)
	defer cancel()

	return b.echo.Shutdown(ctx)
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
}
