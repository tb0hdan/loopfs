package models

// NodeInfo represents system information for a CAS node.
type NodeInfo struct {
	Uptime        string       `json:"uptime"`
	UptimeSeconds int64        `json:"uptime_seconds"`
	LoadAverages  LoadAverages `json:"load_averages"`
	Memory        MemoryInfo   `json:"memory"`
	Storage       StorageInfo  `json:"storage"`
}

// LoadAverages represents system load information.
type LoadAverages struct {
	Load1  float64 `json:"load_1"`
	Load5  float64 `json:"load_5"`
	Load15 float64 `json:"load_15"`
}

// MemoryInfo represents memory usage information.
type MemoryInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}

// StorageInfo represents disk usage information.
type StorageInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
}
