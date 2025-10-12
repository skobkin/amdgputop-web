package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/procscan"
	"github.com/skobkin/amdgputop-web/internal/sampler"
)

type options struct {
	sysfsRoot    string
	debugfsRoot  string
	procRoot     string
	sample       bool
	collectProcs bool
	gpuFilter    string
	jsonOutput   bool
	procCfg      config.ProcConfig
}

func parseFlags(base config.Config) options {
	defaultGPU := base.DefaultGPU
	if defaultGPU == "auto" {
		defaultGPU = ""
	}

	opts := options{
		sysfsRoot:    base.SysfsRoot,
		debugfsRoot:  base.DebugfsRoot,
		procRoot:     base.ProcRoot,
		collectProcs: base.Proc.Enable,
		procCfg:      base.Proc,
	}

	flag.StringVar(&opts.sysfsRoot, "sysfs", opts.sysfsRoot, "Path to sysfs root")
	flag.StringVar(&opts.debugfsRoot, "debugfs", opts.debugfsRoot, "Path to debugfs root")
	flag.StringVar(&opts.procRoot, "proc", opts.procRoot, "Path to procfs root")
	flag.BoolVar(&opts.sample, "sample", false, "Collect one telemetry sample per GPU")
	flag.BoolVar(&opts.collectProcs, "procs", opts.collectProcs, "Collect process snapshots when sampling")
	flag.StringVar(&opts.gpuFilter, "gpu", defaultGPU, "Limit sampling to specific GPU id")
	flag.BoolVar(&opts.jsonOutput, "json", false, "Emit discovery result as JSON")
	flag.Parse()

	opts.procCfg.Enable = opts.collectProcs
	return opts
}

func main() {
	baseCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config defaults: %v\n", err)
		os.Exit(1)
	}

	opts := parseFlags(baseCfg)

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

	selected := make([]gpu.Info, 0, len(infos))
	for _, info := range infos {
		if opts.gpuFilter != "" && opts.gpuFilter != info.ID {
			continue
		}
		selected = append(selected, info)
	}

	if len(selected) == 0 {
		logger.Warn("no GPUs matched selection", "gpu_id", opts.gpuFilter)
		return
	}

	readers := make(map[string]*sampler.Reader, len(selected))
	for _, info := range selected {
		readerLogger := logger.With("component", "sampler_reader", "gpu_id", info.ID)
		reader, err := sampler.NewReader(info.ID, opts.sysfsRoot, opts.debugfsRoot, readerLogger)
		if err != nil {
			logger.Warn("sampler reader init failed", "gpu_id", info.ID, "err", err)
			continue
		}
		readers[info.ID] = reader
	}

	var (
		procManager *procscan.Manager
		procCancel  context.CancelFunc
	)

	if opts.collectProcs {
		procLogger := logger.With("component", "procscan")
		procCfg := opts.procCfg
		if procCfg.ScanInterval <= 0 {
			procCfg.ScanInterval = 250 * time.Millisecond
		}

		procManager, err = procscan.NewManager(procCfg, opts.procRoot, selected, procLogger)
		if err != nil {
			logger.Warn("proc scanner init failed", "err", err)
		} else {
			var procCtx context.Context
			procCtx, procCancel = context.WithCancel(context.Background())
			go func() { _ = procManager.Run(procCtx) }()
			if !waitUntil(2*time.Second, procManager.Ready) {
				logger.Warn("process scanner did not become ready before timeout")
			}
			defer procCancel()
		}
	}

	if len(readers) == 0 && (procManager == nil || !procManager.Ready()) {
		logger.Warn("no telemetry sources available for selected GPUs", "gpu_count", len(selected))
		return
	}

	fmt.Println()
	fmt.Printf("Collecting samples at %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Println(strings.Repeat("-", 60))

	ids := make([]string, 0, len(selected))
	for _, info := range selected {
		ids = append(ids, info.ID)
	}
	sort.Strings(ids)

	for _, id := range ids {
		reader, ok := readers[id]
		if !ok {
			fmt.Printf("GPU %s sample: metrics unavailable (reader init failed)\n\n", id)
			continue
		}
		sample := reader.Sample()
		data, err := json.MarshalIndent(sample, "", "  ")
		if err != nil {
			logger.Error("encode sample", "gpu_id", id, "err", err)
			continue
		}
		fmt.Printf("GPU %s sample:\n%s\n\n", id, string(data))
	}

	if procManager != nil {
		fmt.Println("GPU process snapshots:")
		for _, id := range ids {
			snapshot, ok := procManager.Latest(id)
			if !ok {
				fmt.Printf("- %s: no process data available yet\n", id)
				continue
			}
			data, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				logger.Error("encode process snapshot", "gpu_id", id, "err", err)
				continue
			}
			fmt.Printf("GPU %s processes:\n%s\n", id, string(data))
		}
		fmt.Println()
	}
}

func waitUntil(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return condition()
}
