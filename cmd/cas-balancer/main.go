package main

import (
	"flag"
	"os"
	"strings"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/server/balancer"
)

const (
	defaultRetryMax             = 3
	defaultRetryWaitMax         = 30 * time.Second
	defaultRequestTimeout       = 30 * time.Second
	gracefulShutdownTimeout     = 10 * time.Second
	defaultHealthCheckInterval  = 5 * time.Second
	defaultHealthCheckTimeout   = 5 * time.Second
)

func main() {
	// Initialize logger
	_ = log.Logger

	// Parse command-line flags
	var backends string
	flag.StringVar(&backends, "backends", "", "Comma-separated list of CAS server URLs (e.g., http://server1:8080,http://server2:8080)")
	addr := flag.String("addr", ":8081", "Load balancer listen address")
	retryMax := flag.Int("retry-max", defaultRetryMax, "Maximum number of retries")
	retryWaitMin := flag.Duration("retry-wait-min", 1*time.Second, "Minimum wait time between retries")
	retryWaitMax := flag.Duration("retry-wait-max", defaultRetryWaitMax, "Maximum wait time between retries")
	requestTimeout := flag.Duration("request-timeout", defaultRequestTimeout, "Request timeout")
	healthCheckInterval := flag.Duration("health-check-interval", defaultHealthCheckInterval, "Interval between health checks")
	healthCheckTimeout := flag.Duration("health-check-timeout", defaultHealthCheckTimeout, "Timeout for health check requests")
	debug := flag.Bool("debug", false, "Enable debug logging")
	debugAddr := flag.String("debug-addr", "localhost:6060", "Debug server address (pprof)")
	dbPath := flag.String("db", "", "SQLite database path for bucket metadata (enables bucket API)")

	flag.Parse()

	// Configure logger
	if *debug {
		log.SetDebugMode()
		log.Debug().Msg("Debug mode enabled")
	}

	// Validate backends
	if backends == "" {
		log.Fatal().Msg("At least one backend must be specified with -backends flag")
	}

	backendList := strings.Split(backends, ",")
	for i, backend := range backendList {
		backendList[i] = strings.TrimSpace(backend)
		if !strings.HasPrefix(backendList[i], "http://") && !strings.HasPrefix(backendList[i], "https://") {
			log.Fatal().Str("backend", backendList[i]).Msg("Backend must start with http:// or https://")
		}
	}

	log.Info().
		Strs("backends", backendList).
		Dur("health_check_interval", *healthCheckInterval).
		Dur("health_check_timeout", *healthCheckTimeout).
		Dur("request_timeout", *requestTimeout).
		Msg("Configured backends")

	bServer := balancer.NewBalancerServer(
		backendList,
		*retryMax,
		gracefulShutdownTimeout,
		*retryWaitMin,
		*retryWaitMax,
		*requestTimeout,
		*healthCheckInterval,
		*healthCheckTimeout,
		*debug,
		*debugAddr,
		*dbPath,
	)
	if err := bServer.Start(*addr); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}

	os.Exit(0)
}
