// Package sampler provides GPU telemetry sampling utilities.
package sampler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Manager orchestrates per-GPU samplers, caches the latest snapshot,
// and fan-outs updates to subscribers.
type Manager struct {
	interval time.Duration
	readers  map[string]*Reader
	logger   *slog.Logger

	mu              sync.RWMutex
	latest          map[string]Sample
	subscribers     map[string]map[*subscriber]struct{}
	subscriberCount int
	lastDemandAt    time.Time
	lazy            bool
	idleTTL         time.Duration

	sampleMu  sync.Mutex
	activity  chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// NewManager builds a Manager from pre-constructed readers.
func NewManager(interval time.Duration, readers map[string]*Reader, logger *slog.Logger) (*Manager, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be > 0")
	}
	if logger == nil {
		logger = slog.Default()
	}
	manager := &Manager{
		interval:    interval,
		readers:     readers,
		logger:      logger.With("component", "sampler_manager"),
		latest:      make(map[string]Sample),
		subscribers: make(map[string]map[*subscriber]struct{}),
		activity:    make(chan struct{}, 1),
	}

	return manager, nil
}

// EnableLazy enables on-demand sampling and idle shutdown semantics.
func (m *Manager) EnableLazy(idleTTL time.Duration) error {
	if idleTTL <= 0 {
		return fmt.Errorf("idle ttl must be > 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lazy = true
	m.idleTTL = idleTTL

	return nil
}

// Run starts the sampling loop for all configured GPUs until the context is canceled.
func (m *Manager) Run(ctx context.Context) error {
	if len(m.readers) == 0 {
		<-ctx.Done()

		return m.Close()
	}

	if !m.lazy {
		m.logger.Info("sampler started", "lazy", false)
		m.sampleAll()

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				m.logger.Info("sampler stopping", "reason", ctx.Err())

				return m.Close()
			case <-ticker.C:
				m.sampleAll()
			}
		}
	}

	m.logger.Info("sampler started", "lazy", true, "idle_ttl", m.idleTTL)
	sleeping := false
	for {
		if !m.HasDemand() && !sleeping {
			m.logger.Info("sampler going idle")
			sleeping = true
		}

		if !m.waitUntilDemand(ctx) {
			m.logger.Info("sampler stopping", "reason", ctx.Err())

			return m.Close()
		}
		if sleeping {
			m.logger.Info("sampler resuming from idle")
			sleeping = false
		}

		if !m.allFreshWithin(m.interval) {
			m.sampleAll()
		}

		timer := time.NewTimer(m.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			m.logger.Info("sampler stopping", "reason", ctx.Err())

			return m.Close()
		case <-timer.C:
		}
	}
}

// Latest returns the most recent cached sample for the given GPU.
func (m *Manager) Latest(gpuID string) (Sample, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sample, ok := m.latest[gpuID]

	return sample, ok
}

// Current returns a fresh-enough sample or collects one synchronously.
func (m *Manager) Current(gpuID string) (Sample, bool, error) {
	now := time.Now()
	if sample, ok := m.currentLockedRead(gpuID, now); ok {
		return sample, true, nil
	}

	m.sampleMu.Lock()
	defer m.sampleMu.Unlock()

	if sample, ok := m.currentLockedRead(gpuID, now); ok {
		return sample, true, nil
	}

	reader, ok := m.readers[gpuID]
	if !ok {
		return Sample{}, false, fmt.Errorf("unknown gpu %q", gpuID)
	}

	sample := reader.Sample()
	m.storeSample(sample)
	m.touchDemand(now, true)

	return sample, true, nil
}

// CurrentAll returns fresh-enough samples for all GPUs or refreshes them synchronously.
func (m *Manager) CurrentAll() map[string]Sample {
	now := time.Now()
	if samples, ok := m.currentAllLockedRead(now); ok {
		return samples
	}

	m.sampleMu.Lock()
	defer m.sampleMu.Unlock()

	if samples, ok := m.currentAllLockedRead(now); ok {
		return samples
	}

	for _, sample := range m.sampleAllUnlocked() {
		m.storeSample(sample)
	}
	m.touchDemand(now, true)

	return m.copyLatest()
}

// Subscribe registers a listener for updates on the given GPU.
func (m *Manager) Subscribe(gpuID string) (<-chan Sample, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.readers[gpuID]; !ok {
		return nil, nil, fmt.Errorf("unknown gpu %q", gpuID)
	}

	sub := newSubscriber()
	if _, ok := m.subscribers[gpuID]; !ok {
		m.subscribers[gpuID] = make(map[*subscriber]struct{})
	}
	m.subscribers[gpuID][sub] = struct{}{}
	m.subscriberCount++
	m.lastDemandAt = time.Now()

	if sample, ok := m.latest[gpuID]; ok {
		sub.send(sample)
	}

	m.signalActivity()

	unsubscribe := func() {
		m.removeSubscriber(gpuID, sub)
	}

	return sub.channel(), unsubscribe, nil
}

// GPUIDs returns the list of GPU ids managed by the sampler.
func (m *Manager) GPUIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.readers))
	for id := range m.readers {
		ids = append(ids, id)
	}

	return ids
}

// Ready reports whether all configured samplers have published at least one sample.
func (m *Manager) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.readers) == 0 {
		return true
	}

	for id := range m.readers {
		if _, ok := m.latest[id]; !ok {
			return false
		}
	}

	return true
}

// HasDemand reports whether lazy mode currently considers sampling active.
func (m *Manager) HasDemand() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.hasDemandLocked(time.Now())
}

func (m *Manager) currentLockedRead(gpuID string, now time.Time) (Sample, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.readers[gpuID]; !ok {
		return Sample{}, false
	}

	m.lastDemandAt = now
	sample, ok := m.latest[gpuID]
	if !ok {
		return Sample{}, false
	}
	if m.lazy && now.Sub(sample.Timestamp) > m.idleTTL {
		return Sample{}, false
	}

	return sample, true
}

func (m *Manager) currentAllLockedRead(now time.Time) (map[string]Sample, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastDemandAt = now
	if len(m.readers) == 0 {
		return map[string]Sample{}, true
	}
	if m.lazy {
		for id := range m.readers {
			sample, ok := m.latest[id]
			if !ok || now.Sub(sample.Timestamp) > m.idleTTL {
				return nil, false
			}
		}
	} else {
		for id := range m.readers {
			if _, ok := m.latest[id]; !ok {
				return nil, false
			}
		}
	}

	return m.copyLatestLocked(), true
}

func (m *Manager) waitUntilDemand(ctx context.Context) bool {
	for {
		if m.HasDemand() {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-m.activity:
		}
	}
}

func (m *Manager) hasDemandLocked(now time.Time) bool {
	if !m.lazy {
		return true
	}
	if m.subscriberCount > 0 {
		return true
	}
	if m.lastDemandAt.IsZero() {
		return false
	}

	return now.Sub(m.lastDemandAt) < m.idleTTL
}

func (m *Manager) allFreshWithin(maxAge time.Duration) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.readers) == 0 {
		return true
	}

	now := time.Now()
	for id := range m.readers {
		sample, ok := m.latest[id]
		if !ok || now.Sub(sample.Timestamp) > maxAge {
			return false
		}
	}

	return true
}

func (m *Manager) sampleAll() {
	m.sampleMu.Lock()
	defer m.sampleMu.Unlock()

	for _, sample := range m.sampleAllUnlocked() {
		m.storeSample(sample)
	}
}

func (m *Manager) sampleAllUnlocked() []Sample {
	samples := make([]Sample, 0, len(m.readers))
	for _, reader := range m.readers {
		samples = append(samples, reader.Sample())
	}

	return samples
}

func (m *Manager) storeSample(sample Sample) {
	m.mu.Lock()
	m.latest[sample.GPUId] = sample

	targetSubs := make([]*subscriber, 0, len(m.subscribers[sample.GPUId]))
	for sub := range m.subscribers[sample.GPUId] {
		targetSubs = append(targetSubs, sub)
	}
	m.mu.Unlock()

	for _, sub := range targetSubs {
		sub.send(sample)
	}
}

func (m *Manager) removeSubscriber(gpuID string, sub *subscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if subs, ok := m.subscribers[gpuID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(m.subscribers, gpuID)
		}
	}
	if m.subscriberCount > 0 {
		m.subscriberCount--
	}
	m.lastDemandAt = time.Now()
	sub.close()
	m.signalActivity()
}

func (m *Manager) touchDemand(now time.Time, signal bool) {
	m.mu.Lock()
	m.lastDemandAt = now
	m.mu.Unlock()
	if signal {
		m.signalActivity()
	}
}

func (m *Manager) signalActivity() {
	select {
	case m.activity <- struct{}{}:
	default:
	}
}

func (m *Manager) copyLatest() map[string]Sample {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.copyLatestLocked()
}

func (m *Manager) copyLatestLocked() map[string]Sample {
	out := make(map[string]Sample, len(m.latest))
	for id, sample := range m.latest {
		out[id] = sample
	}

	return out
}

// Close releases all reader resources. Safe for repeated use.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		var errs []error
		for id, reader := range m.readers {
			if reader == nil {
				continue
			}
			if err := reader.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close reader %s: %w", id, err))
			}
		}
		m.closeErr = errors.Join(errs...)
	})

	return m.closeErr
}

type subscriber struct {
	ch     chan Sample
	mu     sync.Mutex
	closed bool
}

func newSubscriber() *subscriber {
	return &subscriber{
		ch: make(chan Sample, 1),
	}
}

func (s *subscriber) channel() <-chan Sample {
	return s.ch
}

func (s *subscriber) send(sample Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- sample:
		return
	default:
		// Drop oldest to make room for new sample.
		select {
		case <-s.ch:
		default:
		}
		select {
		case s.ch <- sample:
		default:
		}
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	close(s.ch)
	s.closed = true
}
