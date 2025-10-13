package procscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
)

// Manager orchestrates process-top scans and fan-out to subscribers.
type Manager struct {
	cfg      config.ProcConfig
	procRoot string
	logger   *slog.Logger

	gpuIDs     []string
	renderNode map[string]string
	lookup     *gpuLookup
	collector  *collector

	mu          sync.RWMutex
	latest      map[string]Snapshot
	subscribers map[string]map[*procSubscriber]struct{}
	prevEngine  map[string]map[int]uint64
	lastScan    time.Time
	closeOnce   sync.Once
	closeErr    error
}

// NewManager constructs a process scanner manager.
func NewManager(cfg config.ProcConfig, procRoot string, gpus []gpu.Info, logger *slog.Logger) (*Manager, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if cfg.ScanInterval <= 0 {
		return nil, fmt.Errorf("scan interval must be > 0")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	gpuIDs := make([]string, 0, len(gpus))
	renderNodes := make(map[string]string, len(gpus))
	for _, info := range gpus {
		gpuIDs = append(gpuIDs, info.ID)
		renderNodes[info.ID] = info.RenderNode
	}

	manager := &Manager{
		cfg:         cfg,
		procRoot:    procRoot,
		logger:      logger.With("component", "procscan_manager"),
		gpuIDs:      gpuIDs,
		renderNode:  renderNodes,
		lookup:      newGPULookup(gpuIDs, renderNodes),
		latest:      make(map[string]Snapshot),
		subscribers: make(map[string]map[*procSubscriber]struct{}),
		prevEngine:  make(map[string]map[int]uint64),
	}
	coll, err := newCollector(procRoot, cfg.MaxPIDs, cfg.MaxFDsPerPID, manager.lookup, logger.With("component", "procscan_collector"))
	if err != nil {
		return nil, fmt.Errorf("init collector: %w", err)
	}
	manager.collector = coll
	return manager, nil
}

// Run starts the periodic /proc scanner until the context is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	if !m.cfg.Enable || len(m.gpuIDs) == 0 {
		<-ctx.Done()
		return m.Close()
	}

	m.logger.Info("process scanner started", "interval", m.cfg.ScanInterval)
	m.performScan(time.Now())

	ticker := time.NewTicker(m.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("process scanner stopping", "reason", ctx.Err())
			return m.Close()
		case now := <-ticker.C:
			m.performScan(now)
		}
	}
}

// Latest returns the most recent snapshot for the supplied GPU.
func (m *Manager) Latest(gpuID string) (Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot, ok := m.latest[gpuID]
	return snapshot, ok
}

// Subscribe registers for process snapshot updates for the supplied GPU.
func (m *Manager) Subscribe(gpuID string) (<-chan Snapshot, func(), error) {
	if !m.cfg.Enable {
		return nil, nil, fmt.Errorf("process scanner disabled")
	}

	if !m.knowsGPU(gpuID) {
		return nil, nil, fmt.Errorf("unknown gpu %q", gpuID)
	}

	sub := newProcSubscriber()

	m.mu.Lock()
	if _, ok := m.subscribers[gpuID]; !ok {
		m.subscribers[gpuID] = make(map[*procSubscriber]struct{})
	}
	m.subscribers[gpuID][sub] = struct{}{}

	if snapshot, ok := m.latest[gpuID]; ok {
		sub.send(snapshot)
	}
	m.mu.Unlock()

	unsubscribe := func() {
		m.removeSubscriber(gpuID, sub)
	}
	return sub.channel(), unsubscribe, nil
}

// GPUIDs enumerates GPUs tracked by the manager.
func (m *Manager) GPUIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, len(m.gpuIDs))
	copy(ids, m.gpuIDs)
	return ids
}

// Ready reports whether at least one scan has been performed.
func (m *Manager) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.lastScan.IsZero()
}

func (m *Manager) performScan(now time.Time) {
	collections, err := m.collector.collect()
	if err != nil {
		m.logger.Warn("process scan failed", "err", err)
		return
	}

	m.mu.RLock()
	prevScan := m.lastScan
	m.mu.RUnlock()

	var elapsedSeconds float64
	if !prevScan.IsZero() {
		elapsed := now.Sub(prevScan)
		if elapsed <= 0 {
			elapsed = m.cfg.ScanInterval
		}
		elapsedSeconds = elapsed.Seconds()
	}

	for _, gpuID := range m.gpuIDs {
		col := collections[gpuID]
		prev := m.getPrevEngine(gpuID)

		processes := make([]Process, 0, len(col.processes))
		nextTotals := make(map[int]uint64)

		for _, raw := range col.processes {
			proc := Process{
				PID:        raw.pid,
				UID:        raw.uid,
				User:       raw.user,
				Name:       raw.name,
				Command:    raw.command,
				RenderNode: raw.renderNode,
			}

			if raw.hasMemory {
				vram := raw.vramBytes
				gtt := raw.gttBytes
				proc.VRAMBytes = &vram
				proc.GTTBytes = &gtt
			}

			if raw.hasEngine {
				nextTotals[raw.pid] = raw.engineTotal
				if elapsedSeconds > 0 {
					if prevTotal, ok := prev[raw.pid]; ok && raw.engineTotal >= prevTotal {
						deltaNS := raw.engineTotal - prevTotal
						ms := float64(deltaNS) / 1_000_000
						value := ms / elapsedSeconds
						proc.GPUTimeMSPerS = &value
					}
				}
			}

			processes = append(processes, proc)
		}

		sort.Slice(processes, func(i, j int) bool {
			var vi, vj uint64
			if processes[i].VRAMBytes != nil {
				vi = *processes[i].VRAMBytes
			}
			if processes[j].VRAMBytes != nil {
				vj = *processes[j].VRAMBytes
			}
			if vi == vj {
				return processes[i].PID < processes[j].PID
			}
			return vi > vj
		})

		snapshot := Snapshot{
			GPUId:     gpuID,
			Timestamp: now.UTC(),
			Capabilities: Capabilities{
				VRAMGTTFromFDInfo:    col.hasMemory,
				EngineTimeFromFDInfo: col.hasEngine,
			},
			Processes: processes,
		}

		if len(nextTotals) == 0 {
			m.publish(snapshot, nil)
		} else {
			m.publish(snapshot, nextTotals)
		}
	}

	m.mu.Lock()
	m.lastScan = now
	m.mu.Unlock()
}

func (m *Manager) publish(snapshot Snapshot, engineTotals map[int]uint64) {
	m.mu.Lock()
	m.latest[snapshot.GPUId] = snapshot
	if engineTotals == nil {
		delete(m.prevEngine, snapshot.GPUId)
	} else {
		m.prevEngine[snapshot.GPUId] = engineTotals
	}
	subs := make([]*procSubscriber, 0, len(m.subscribers[snapshot.GPUId]))
	for sub := range m.subscribers[snapshot.GPUId] {
		subs = append(subs, sub)
	}
	m.mu.Unlock()

	for _, sub := range subs {
		sub.send(snapshot)
	}
}

func (m *Manager) getPrevEngine(gpuID string) map[int]uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if totals, ok := m.prevEngine[gpuID]; ok {
		return totals
	}
	return nil
}

func (m *Manager) removeSubscriber(gpuID string, sub *procSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if subs, ok := m.subscribers[gpuID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(m.subscribers, gpuID)
		}
	}
	sub.close()
}

// Close releases any filesystem handles retained by the manager.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		var errs []error
		if m.collector != nil {
			if err := m.collector.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close collector: %w", err))
			}
		}
		m.closeErr = errors.Join(errs...)
	})
	return m.closeErr
}

func (m *Manager) knowsGPU(gpuID string) bool {
	for _, id := range m.gpuIDs {
		if id == gpuID {
			return true
		}
	}
	return false
}

type procSubscriber struct {
	ch     chan Snapshot
	mu     sync.Mutex
	closed bool
}

func newProcSubscriber() *procSubscriber {
	return &procSubscriber{
		ch: make(chan Snapshot, 1),
	}
}

func (s *procSubscriber) channel() <-chan Snapshot {
	return s.ch
}

func (s *procSubscriber) send(snapshot Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- snapshot:
	default:
		select {
		case <-s.ch:
		default:
		}
		select {
		case s.ch <- snapshot:
		default:
		}
	}
}

func (s *procSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	close(s.ch)
	s.closed = true
}
