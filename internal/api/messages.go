package api

import (
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/procscan"
	"github.com/skobkin/amdgputop-web/internal/sampler"
)

// HelloMessage is the initial payload sent on WebSocket connection.
type HelloMessage struct {
	Type       string          `json:"type"`
	IntervalMS int             `json:"interval_ms"`
	GPUs       []gpu.Info      `json:"gpus"`
	Features   map[string]bool `json:"features"`
}

// NewHelloMessage constructs a hello payload.
func NewHelloMessage(intervalMS int, gpus []gpu.Info, features map[string]bool) HelloMessage {
	return HelloMessage{
		Type:       "hello",
		IntervalMS: intervalMS,
		GPUs:       gpus,
		Features:   features,
	}
}

// StatsMessage wraps a sampler snapshot for transport.
type StatsMessage struct {
	Type string `json:"type"`
	sampler.Sample
}

// NewStatsMessage constructs a stats payload.
func NewStatsMessage(sample sampler.Sample) StatsMessage {
	return StatsMessage{
		Type:   "stats",
		Sample: sample,
	}
}

// ProcsMessage wraps a process snapshot for transport.
type ProcsMessage struct {
	Type string `json:"type"`
	procscan.Snapshot
}

// NewProcsMessage constructs a procs payload.
func NewProcsMessage(snapshot procscan.Snapshot) ProcsMessage {
	return ProcsMessage{
		Type:     "procs",
		Snapshot: snapshot,
	}
}

// ErrorMessage communicates an error condition to the client.
type ErrorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ClientMessage is a generic envelope used for decoding inbound client messages.
type ClientMessage struct {
	Type string `json:"type"`
}

// SubscribeMessage requests subscription to GPU telemetry.
type SubscribeMessage struct {
	Type  string `json:"type"`
	GPUId string `json:"gpu_id"`
}

// PongMessage is the response to a ping.
type PongMessage struct {
	Type string `json:"type"`
}
