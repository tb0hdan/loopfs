package casd

import (
	"context"
	"errors"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/manager"
	"loopfs/pkg/store"
	"loopfs/pkg/store/loop"

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
	storeMgr   *manager.Manager
	debug      bool
	debugAddr  string
}

func NewCASServer(storageDir, webDir, version string, storeImpl store.Store, debug bool, debugAddr string) *CASServer {
	return NewCASServerWithTempDir(storageDir, filepath.Join(storageDir, "temp"), webDir, version, storeImpl, debug, debugAddr)
}

func NewCASServerWithTempDir(storageDir, tempDir, webDir, version string, storeImpl store.Store, debug bool, debugAddr string) *CASServer {
	// Check if storeImpl is actually a Store Manager
	var storeMgr *manager.Manager
	if mgr, ok := storeImpl.(*manager.Manager); ok {
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
		debug:      debug,
		debugAddr:  debugAddr,
	}
}

func (cas *CASServer) Start(addr string) error {
	cas.setupRoutes()
	// Start pprof server if in debug mode
	if cas.debug {
		go func() {
			log.Info().Msgf("Starting pprof server on %s", cas.debugAddr)
			log.Info().Msgf("%+v", http.ListenAndServe(cas.debugAddr, nil)) //nolint:gosec
		}()
	}
	// Start the server in a goroutine.
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

	// Wait for the interrupt signal to gracefully shutdown the server
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

	// Unmount all currently mounted loop images
	if loopStore, ok := cas.store.(*loop.Store); ok {
		log.Info().Msg("Unmounting all loop images...")
		if err := loopStore.UnmountAll(); err != nil {
			log.Error().Err(err).Msg("Failed to unmount all loop images")
			// Continue with shutdown even if unmount fails
		} else {
			log.Info().Msg("All loop images unmounted successfully")
		}
	} else if cas.storeMgr != nil {
		// If using store manager, check if the underlying store is a loop store
		if loopStore, ok := cas.storeMgr.GetStore().(*loop.Store); ok {
			log.Info().Msg("Unmounting all loop images...")
			if err := loopStore.UnmountAll(); err != nil {
				log.Error().Err(err).Msg("Failed to unmount all loop images")
				// Continue with shutdown even if unmount fails
			} else {
				log.Info().Msg("All loop images unmounted successfully")
			}
		}
	}

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
	cas.echo.GET("/node/info", cas.getNodeInfo)
	cas.echo.POST("/file/upload", cas.uploadFile)
	cas.echo.GET("/file/:hash/download", cas.downloadFile)
	cas.echo.GET("/file/:hash/info", cas.getFileInfo)
	cas.echo.DELETE("/file/:hash/delete", cas.deleteFile)
}
