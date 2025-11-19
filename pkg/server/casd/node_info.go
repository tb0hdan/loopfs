package casd

import (
	"bufio"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/models"

	"github.com/labstack/echo/v4"
)

// getNodeInfo handles the GET /node/info endpoint.
func (cas *CASServer) getNodeInfo(ctx echo.Context) error {
	info, err := cas.collectNodeInfo()
	if err != nil {
		log.Error().Err(err).Msg("Failed to collect node information")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to collect node information",
		})
	}

	return ctx.JSON(http.StatusOK, info)
}

// collectNodeInfo gathers system information.
func (cas *CASServer) collectNodeInfo() (*models.NodeInfo, error) {
	uptime, err := getUptime()
	if err != nil {
		return nil, err
	}

	loadAvg, err := getLoadAverages()
	if err != nil {
		return nil, err
	}

	memory, err := getMemoryInfo()
	if err != nil {
		return nil, err
	}

	storage, err := getStorageInfo(cas.storageDir)
	if err != nil {
		return nil, err
	}

	return &models.NodeInfo{
		Uptime:        formatUptime(uptime),
		UptimeSeconds: uptime,
		LoadAverages:  *loadAvg,
		Memory:        *memory,
		Storage:       *storage,
	}, nil
}

// getUptime reads system uptime from /proc/uptime.
func getUptime() (int64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, err
	}

	uptimeFloat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}

	return int64(uptimeFloat), nil
}

// getLoadAverages reads load averages from /proc/loadavg.
func getLoadAverages() (*models.LoadAverages, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}

	const minLoadFields = 3
	fields := strings.Fields(string(data))
	if len(fields) < minLoadFields {
		return nil, err
	}

	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}

	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, err
	}

	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, err
	}

	return &models.LoadAverages{
		Load1:  load1,
		Load5:  load5,
		Load15: load15,
	}, nil
}

// getMemoryInfo reads memory information from /proc/meminfo.
func getMemoryInfo() (*models.MemoryInfo, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close /proc/meminfo file")
		}
	}()

	memStats, err := parseMemInfo(file)
	if err != nil {
		return nil, err
	}

	// Use MemAvailable if available (more accurate), otherwise calculate
	available := memStats.Available
	if available == 0 {
		available = memStats.Free + memStats.Buffers + memStats.Cached
	}

	used := memStats.Total - available

	return &models.MemoryInfo{
		Total:     memStats.Total,
		Used:      used,
		Available: available,
	}, nil
}

type memStatValues struct {
	Total     uint64
	Free      uint64
	Available uint64
	Buffers   uint64
	Cached    uint64
}

func parseMemInfo(file *os.File) (*memStatValues, error) {
	var stats memStatValues

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		const minMemFields = 2
		fields := strings.Fields(line)
		if len(fields) < minMemFields {
			continue
		}

		key := strings.TrimSuffix(fields[0], ":")
		valueStr := fields[1]
		value, err := strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			continue
		}

		const kbToBytes = 1024
		// Convert from kB to bytes
		value *= kbToBytes

		parseMemValue(key, value, &stats)
	}

	return &stats, scanner.Err()
}

func parseMemValue(key string, value uint64, stats *memStatValues) {
	switch key {
	case "MemTotal":
		stats.Total = value
	case "MemFree":
		stats.Free = value
	case "MemAvailable":
		stats.Available = value
	case "Buffers":
		stats.Buffers = value
	case "Cached":
		stats.Cached = value
	}
}

// getStorageInfo gets disk usage information for the specified directory.
func getStorageInfo(path string) (*models.StorageInfo, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return nil, err
	}

	// Convert syscall values to uint64 safely
	blockSize := uint64(stat.Bsize) // #nosec G115 - syscall values are system dependent

	total := stat.Blocks * blockSize
	available := stat.Bavail * blockSize
	used := total - available

	return &models.StorageInfo{
		Total:     total,
		Used:      used,
		Available: available,
	}, nil
}

// formatUptime converts seconds to human-readable format.
func formatUptime(seconds int64) string {
	duration := time.Duration(seconds) * time.Second
	const hoursInDay = 24
	const minutesInHour = 60
	days := int(duration.Hours()) / hoursInDay
	hours := int(duration.Hours()) % hoursInDay
	minutes := int(duration.Minutes()) % minutesInHour

	switch {
	case days > 0:
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	case hours > 0:
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	default:
		return strconv.Itoa(minutes) + "m"
	}
}
