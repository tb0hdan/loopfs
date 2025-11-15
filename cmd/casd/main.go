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
	flag.Parse()

	if err := os.MkdirAll(*storageDir, storageDirPerm); err != nil {
		log.Fatal().Err(err).Str("storage_dir", *storageDir).Msg("Failed to create storage directory")
	}

	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		log.Fatal().Str("web_dir", *webDir).Msg("Web directory does not exist")
	}

	loopStore := loop.New(*storageDir)
	cas := server.NewCASServer(*storageDir, *webDir, strings.TrimSpace(Version), loopStore)

	if err := cas.Start(*port); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}

	os.Exit(0)
}
