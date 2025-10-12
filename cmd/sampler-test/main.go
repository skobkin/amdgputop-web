package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/sampler"
)

type options struct {
	sysfsRoot   string
	debugfsRoot string
	sample      bool
	gpuFilter   string
	jsonOutput  bool
}

func parseFlags() options {
	defaultSysfs := envOrDefault("APP_SYSFS_ROOT", "/sys")
	defaultDebug := envOrDefault("APP_DEBUGFS_ROOT", "/sys/kernel/debug")
	defaultGPU := envOrDefault("APP_DEFAULT_GPU", "")

	var opts options
	flag.StringVar(&opts.sysfsRoot, "sysfs", defaultSysfs, "Path to sysfs root")
	flag.StringVar(&opts.debugfsRoot, "debugfs", defaultDebug, "Path to debugfs root")
	flag.BoolVar(&opts.sample, "sample", false, "Collect one telemetry sample per GPU")
	flag.StringVar(&opts.gpuFilter, "gpu", defaultGPU, "Limit sampling to specific GPU id")
	flag.BoolVar(&opts.jsonOutput, "json", false, "Emit discovery result as JSON")
	flag.Parse()
	return opts
}

func main() {
	opts := parseFlags()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	infos, err := gpu.Discover(opts.sysfsRoot, logger.With("component", "gpu_discovery"))
	if err != nil {
		logger.Error("gpu discovery failed", "err", err)
		os.Exit(1)
	}

	if opts.jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(infos); err != nil {
			logger.Error("encode discovery output", "err", err)
			os.Exit(1)
		}
	} else {
		if len(infos) == 0 {
			fmt.Println("No GPUs detected")
		} else {
			fmt.Println("Discovered GPUs:")
		}
		for _, info := range infos {
			fmt.Printf("- %s (PCI: %s, PCIID: %s, Render: %s, Name: %s)\n", info.ID, info.PCI, info.PCIID, info.RenderNode, info.Name)
		}
	}

	if !opts.sample {
		return
	}

	readers := make(map[string]*sampler.Reader, len(infos))
	for _, info := range infos {
		if opts.gpuFilter != "" && opts.gpuFilter != info.ID {
			continue
		}
		readerLogger := logger.With("component", "sampler_reader", "gpu_id", info.ID)
		reader, err := sampler.NewReader(info.ID, opts.sysfsRoot, opts.debugfsRoot, readerLogger)
		if err != nil {
			logger.Warn("sampler reader init failed", "gpu_id", info.ID, "err", err)
			continue
		}
		readers[info.ID] = reader
	}

	if len(readers) == 0 {
		logger.Warn("no sampler readers initialised", "count", len(infos))
		return
	}

	fmt.Println()
	fmt.Printf("Collecting samples at %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Println(strings.Repeat("-", 60))

	for id, reader := range readers {
		sample := reader.Sample()
		data, err := json.MarshalIndent(sample, "", "  ")
		if err != nil {
			logger.Error("encode sample", "gpu_id", id, "err", err)
			continue
		}
		fmt.Printf("GPU %s sample:\n%s\n\n", id, string(data))
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
