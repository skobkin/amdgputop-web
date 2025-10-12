package procscan

import "time"

// Snapshot represents a single process-top snapshot for a GPU.
type Snapshot struct {
	GPUId        string       `json:"gpu_id"`
	Timestamp    time.Time    `json:"ts"`
	Capabilities Capabilities `json:"capabilities"`
	Processes    []Process    `json:"processes"`
}

// Capabilities describes which metrics could be collected during a scan.
type Capabilities struct {
	VRAMGTTFromFDInfo    bool `json:"vram_gtt_from_fdinfo"`
	EngineTimeFromFDInfo bool `json:"engine_time_from_fdinfo"`
}

// Process summarises GPU memory usage for a process observed via fdinfo.
type Process struct {
	PID           int      `json:"pid"`
	UID           int      `json:"uid"`
	User          string   `json:"user"`
	Name          string   `json:"name"`
	Command       string   `json:"cmd"`
	RenderNode    string   `json:"render_node"`
	VRAMBytes     *uint64  `json:"vram_bytes"`
	GTTBytes      *uint64  `json:"gtt_bytes"`
	GPUTimeMSPerS *float64 `json:"gpu_time_ms_per_s"`
}
