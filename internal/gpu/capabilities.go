package gpu

// Capabilities describes which telemetry metric sources a GPU exposes.
// A false flag means the metric is known to be unsupported by this
// GPU/platform and may be hidden by consumers; a true flag with an
// unavailable sample value means "supported but currently unavailable".
//
// VRAM and GTT reflect the availability of their mem_info_*_used sources;
// the corresponding total files are auxiliary and only improve the
// displayed usage ratio.
type Capabilities struct {
	GPUBusyPct bool `json:"gpu_busy_pct"`
	MemBusyPct bool `json:"mem_busy_pct"`
	SCLKMHz    bool `json:"sclk_mhz"`
	MCLKMHz    bool `json:"mclk_mhz"`
	TempC      bool `json:"temp_c"`
	FanRPM     bool `json:"fan_rpm"`
	PowerW     bool `json:"power_w"`
	VRAM       bool `json:"vram"`
	GTT        bool `json:"gtt"`
}
