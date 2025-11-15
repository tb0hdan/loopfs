package main

import (
	_ "embed"
	"flag"
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/server"
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

	storageDir := flag.String("storage", "build/data", "Storage directory path")
	webDir := flag.String("web", "web", "Web assets directory path")
	port := flag.String("port", "8080", "Server port")
	loopFileSize := flag.Int64("loopsize", oneGB, "Loop file size in megabytes")
	flag.Parse()

	// Check if running as root
	if os.Getuid() != 0 {
		log.Fatal().Msg("casd must be run as root")
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(*storageDir, storageDirPerm); err != nil {
		log.Fatal().Err(err).Str("storage_dir", *storageDir).Msg("Failed to create storage directory")
	}

	// Check if web directory exists
	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		log.Fatal().Str("web_dir", *webDir).Msg("Web directory does not exist")
	}

	loopStore := loop.New(*storageDir, *loopFileSize)
	cas := server.NewCASServer(*storageDir, *webDir, strings.TrimSpace(Version), loopStore)

	if err := cas.Start(*port); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}

	os.Exit(0)
}
