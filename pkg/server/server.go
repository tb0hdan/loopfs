package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

const (
	shutdownTimeout = 10
)

type CASServer struct {
	storageDir string
	webDir     string
	echo       *echo.Echo
	version    string
	store      store.Store
}

func NewCASServer(storageDir, webDir, version string, storeImpl store.Store) *CASServer {
	return &CASServer{
		storageDir: storageDir,
		webDir:     webDir,
		echo:       echo.New(),
		version:    version,
		store:      storeImpl,
	}
}

func (cas *CASServer) Start(port string) error {
	cas.setupRoutes()

	// Start server in a goroutine
	go func() {
		log.Info().
			Str("port", port).
			Str("storage_dir", cas.storageDir).
			Str("web_dir", cas.webDir).
			Msg("Starting CAS server")

		if err := cas.echo.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("Server startup failed")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	return cas.Shutdown()
}

func (cas *CASServer) Shutdown() error {
	log.Info().Msg("Shutting down server...")

	// Gracefully shutdown Echo with a timeout of 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
	defer cancel()

	if err := cas.echo.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown failed")
		return err
	}

	log.Info().Msg("Server gracefully stopped")

	// Execute sync command to flush filesystem buffers
	log.Info().Msg("Executing sync command...")
	cmd := exec.Command("sync")
	if err := cmd.Run(); err != nil {
		log.Warn().Err(err).Msg("Sync command failed")
	} else {
		log.Info().Msg("Filesystem buffers flushed successfully")
	}

	log.Info().Msg("Shutdown complete")
	return nil
}

func (cas *CASServer) setupRoutes() {
	cas.echo.HideBanner = true
	// Setup middleware with custom logger
	cas.echo.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${time_rfc3339} ${status} ${method} ${uri} (${latency_human})\n",
	}))
	cas.echo.Use(middleware.Recover())

	// Setup routes
	cas.echo.GET("/", cas.serveSwaggerUI)
	cas.echo.GET("/swagger.yml", cas.serveSwaggerSpec)
	cas.echo.POST("/file/upload", cas.uploadFile)
	cas.echo.GET("/file/:hash/download", cas.downloadFile)
	cas.echo.GET("/file/:hash/info", cas.getFileInfo)
}
