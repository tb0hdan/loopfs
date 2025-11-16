package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
	"loopfs/pkg/storemanager"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	shutdownTimeout = 10
	syncTimeout     = 30
)

type CASServer struct {
	storageDir string
	tempDir    string // Directory for temporary files during uploads
	webDir     string
	echo       *echo.Echo
	version    string
	store      store.Store
	storeMgr   *storemanager.Manager
}

func NewCASServer(storageDir, webDir, version string, storeImpl store.Store) *CASServer {
	return NewCASServerWithTempDir(storageDir, filepath.Join(storageDir, "temp"), webDir, version, storeImpl)
}

func NewCASServerWithTempDir(storageDir, tempDir, webDir, version string, storeImpl store.Store) *CASServer {
	// Check if storeImpl is actually a Store Manager
	var storeMgr *storemanager.Manager
	if mgr, ok := storeImpl.(*storemanager.Manager); ok {
		storeMgr = mgr
	}

	return &CASServer{
		storageDir: storageDir,
		tempDir:    tempDir,
		webDir:     webDir,
		echo:       echo.New(),
		version:    version,
		store:      storeImpl,
		storeMgr:   storeMgr,
	}
}

func (cas *CASServer) Start(addr string) error {
	cas.setupRoutes()

	// Start server in a goroutine
	go func() {
		log.Info().
			Str("addr", addr).
			Str("storage_dir", cas.storageDir).
			Str("version", cas.version).
			Str("web_dir", cas.webDir).
			Msg("Starting CAS server")

		if err := cas.echo.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	// Execute sync command to flush filesystem buffers with a fresh context
	log.Info().Msg("Executing sync command...")
	syncCtx, syncCancel := context.WithTimeout(context.Background(), syncTimeout*time.Second)
	defer syncCancel()

	cmd := exec.CommandContext(syncCtx, "sync")
	if err := cmd.Run(); err != nil {
		log.Warn().Err(err).Msg("Sync command failed")
	} else {
		log.Info().Msg("Filesystem buffers flushed successfully")
	}

	log.Info().Msg("Shutdown complete")
	return nil
}

func (cas *CASServer) setupRoutes() {
	// Echo configuration
	cas.echo.HideBanner = true
	cas.echo.HidePort = true
	// Setup middleware with custom logger
	cas.echo.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${time_rfc3339} ${status} ${method} ${uri} (${latency_human})\n",
	}))

	//  The server must not gzip every response globally.
	//  File downloads therefore return compressed bytes whenever the client advertises Accept-Encoding: gzip, i.e., default curl/wget behavior.
	//  Clients expecting to receive the exact stored bytes (as implied by a CAS) instead get a re-encoded stream and
	//  must remember to decompress it, defeating the “download raw blob by hash” contract and burning CPU.
	// cas.echo.Use(middleware.Gzip())

	cas.echo.Use(middleware.Recover())

	// Setup routes
	cas.echo.GET("/", cas.serveSwaggerUI)
	cas.echo.GET("/swagger.yml", cas.serveSwaggerSpec)
	cas.echo.POST("/file/upload", cas.uploadFile)
	cas.echo.GET("/file/:hash/download", cas.downloadFile)
	cas.echo.GET("/file/:hash/info", cas.getFileInfo)
	cas.echo.DELETE("/file/:hash/delete", cas.deleteFile)
}
