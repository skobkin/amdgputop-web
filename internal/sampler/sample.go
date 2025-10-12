package sampler

import "time"

// Sample represents a single telemetry snapshot for a GPU.
type Sample struct {
	GPUId     string    `json:"gpu_id"`
	Timestamp time.Time `json:"ts"`
	Metrics   Metrics   `json:"metrics"`
}

// Metrics contains GPU telemetry values. Pointer fields serialize as null when unavailable.
type Metrics struct {
	GPUBusyPct     *float64 `json:"gpu_busy_pct"`
	MemBusyPct     *float64 `json:"mem_busy_pct"`
	SCLKMHz        *float64 `json:"sclk_mhz"`
	MCLKMHz        *float64 `json:"mclk_mhz"`
	TempC          *float64 `json:"temp_c"`
	FanRPM         *float64 `json:"fan_rpm"`
	PowerW         *float64 `json:"power_w"`
	VRAMUsedBytes  *uint64  `json:"vram_used_bytes"`
	VRAMTotalBytes *uint64  `json:"vram_total_bytes"`
	GTTUsedBytes   *uint64  `json:"gtt_used_bytes"`
	GTTTotalBytes  *uint64  `json:"gtt_total_bytes"`
}
