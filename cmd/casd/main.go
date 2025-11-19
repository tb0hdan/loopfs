package main

import (
	_ "embed"
	"flag"
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/manager"
	"loopfs/pkg/server/casd"
	"loopfs/pkg/store/loop"
)

const (
	oneGB          = 1024
	storageDirPerm = 0750
)

//go:embed VERSION
var Version string

func main() {
	// Initialize logger first
	_ = log.Logger

	storageDir := flag.String("storage", "/data/cas", "Storage directory path")
	webDir := flag.String("web", "web", "Web assets directory path")
	addr := flag.String("addr", "127.0.0.1:8080", "Server addr")
	loopFileSize := flag.Int64("loop-size", oneGB, "Loop file size in megabytes (defaults to 1024)")
	debug := flag.Bool("debug", false, "Debug mode")
	debugAddr := flag.String("debug-addr", "localhost:6060", "Debug server address (pprof)")

	// Timeout configuration flags - use default values from the loop package
	defaultTimeouts := loop.DefaultTimeoutConfig()
	baseTimeout := flag.Duration("base-timeout", defaultTimeouts.BaseCommandTimeout, "Timeout for fast operations (mount, unmount, stat)")
	ddTimeoutPerGB := flag.Duration("dd-timeout-per-gb", defaultTimeouts.DDTimeoutPerGB, "Timeout per GB for dd operations")
	mkfsTimeoutPerGB := flag.Duration("mkfs-timeout-per-gb", defaultTimeouts.MkfsTimeoutPerGB, "Timeout per GB for mkfs operations")
	rsyncTimeoutPerGB := flag.Duration("rsync-timeout-per-gb", defaultTimeouts.RsyncTimeoutPerGB, "Timeout per GB for rsync operations")
	minLongTimeout := flag.Duration("min-long-timeout", defaultTimeouts.MinLongOpTimeout, "Minimum timeout for long operations")
	maxLongTimeout := flag.Duration("max-long-timeout", defaultTimeouts.MaxLongOpTimeout, "Maximum timeout for long operations")
	mountCacheTTL := flag.Duration("mount-ttl", loop.DefaultMountCacheTTL(), "Duration to keep loop mounts active after the last request")

	flag.Parse()

	// Configure logger
	if *debug {
		log.SetDebugMode()
		log.Debug().Msg("Debug mode enabled")
	}
	// Check if running as root
	if os.Getuid() != 0 {
		log.Fatal().Msg("casd must be run as root")
	}

	// Ensure a storage directory exists.
	if err := os.MkdirAll(*storageDir, storageDirPerm); err != nil {
		log.Fatal().Err(err).Str("storage_dir", *storageDir).Msg("Failed to create storage directory")
	}

	// Check if web directory exists
	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		log.Fatal().Str("web_dir", *webDir).Msg("Web directory does not exist")
	}

	// Create timeout configuration from command-line flags
	timeoutConfig := loop.TimeoutConfig{
		BaseCommandTimeout: *baseTimeout,
		DDTimeoutPerGB:     *ddTimeoutPerGB,
		MkfsTimeoutPerGB:   *mkfsTimeoutPerGB,
		RsyncTimeoutPerGB:  *rsyncTimeoutPerGB,
		MinLongOpTimeout:   *minLongTimeout,
		MaxLongOpTimeout:   *maxLongTimeout,
	}

	loopStore := loop.New(*storageDir, *loopFileSize, timeoutConfig, *mountCacheTTL)
	// Initialize Store Manager with the default buffer size (128MB)
	storeMgr := manager.New(loopStore, manager.DefaultBufferSize)
	cas := casd.NewCASServer(*storageDir, *webDir, strings.TrimSpace(Version), storeMgr, *debug, *debugAddr)

	if err := cas.Start(*addr); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}

	os.Exit(0)
}
