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

	mu          sync.RWMutex
	latest      map[string]Sample
	subscribers map[string]map[*subscriber]struct{}
	closeOnce   sync.Once
	closeErr    error
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
	}
	return manager, nil
}

// Run starts sampling loops for all configured GPUs until the context is canceled.
func (m *Manager) Run(ctx context.Context) error {
	if len(m.readers) == 0 {
		<-ctx.Done()
		return m.Close()
	}

	var wg sync.WaitGroup
	for gpuID, reader := range m.readers {
		wg.Add(1)
		go func(id string, rdr *Reader) {
			defer wg.Done()
			logger := m.logger.With("gpu_id", id)
			logger.Info("sampler started")

			// Initial sample to prime cache.
			m.storeSample(rdr.Sample())

			ticker := time.NewTicker(m.interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					logger.Info("sampler stopping", "reason", ctx.Err())
					return
				case <-ticker.C:
					m.storeSample(rdr.Sample())
				}
			}
		}(gpuID, reader)
	}

	<-ctx.Done()
	wg.Wait()
	return m.Close()
}

// Latest returns the most recent sample for the given GPU.
func (m *Manager) Latest(gpuID string) (Sample, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sample, ok := m.latest[gpuID]
	return sample, ok
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

	if sample, ok := m.latest[gpuID]; ok {
		sub.send(sample)
	}

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
	sub.close()
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
