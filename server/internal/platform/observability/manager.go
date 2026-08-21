package observabilityplatform

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	domainobs "example.com/gin-vben-admin/server/internal/domain/observability"
)

var ErrManagerClosed = errors.New("observability manager is closed")

// Manager keeps a stable transport reference while allowing the process to
// replace its collector runtime during startup. Reload constructs and
// validates the replacement before swapping it, so a rejected configuration
// cannot disturb the last working runtime.
type Manager struct {
	mu      sync.RWMutex
	runtime *Runtime
	closed  bool
}

func NewManager(config domainobs.Config) (*Manager, error) {
	runtime, err := NewRuntime(config)
	if err != nil {
		return nil, err
	}
	return &Manager{runtime: runtime}, nil
}

func (m *Manager) Reload(config domainobs.Config) error {
	if m == nil {
		return ErrManagerClosed
	}
	replacement, err := NewRuntime(config)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = replacement.Close()
		return ErrManagerClosed
	}
	previous := m.runtime
	m.runtime = replacement
	m.mu.Unlock()

	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (m *Manager) CollectorCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.runtime == nil {
		return 0
	}
	return m.runtime.CollectorCount()
}

func (m *Manager) RecordHTTP(method, route string, status int, duration time.Duration, requestID ...string) {
	if m == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.runtime != nil {
		m.runtime.RecordHTTP(method, route, status, duration, requestID...)
	}
}

func (m *Manager) ServeMetrics(writer http.ResponseWriter, request *http.Request) {
	if m == nil {
		http.NotFound(writer, request)
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.runtime == nil {
		http.NotFound(writer, request)
		return
	}
	m.runtime.ServeMetrics(writer, request)
}

func (m *Manager) Flush(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.runtime == nil {
		return nil
	}
	return m.runtime.Flush(ctx)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	runtime := m.runtime
	m.runtime = nil
	m.mu.Unlock()
	if runtime != nil {
		return runtime.Close()
	}
	return nil
}
