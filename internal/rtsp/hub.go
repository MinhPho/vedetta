package rtsp

import (
	"context"
	"log/slog"
	"sync"
)

// Hub manages one Source per RTSP URL, ensuring a single connection per stream.
type Hub struct {
	mu      sync.Mutex
	sources map[string]*managedSource
	ctx     context.Context
	cancel  context.CancelFunc
}

type managedSource struct {
	source *Source
	cancel context.CancelFunc
}

// NewHub creates a new RTSP hub.
func NewHub(ctx context.Context) *Hub {
	ctx, cancel := context.WithCancel(ctx)
	return &Hub{
		sources: make(map[string]*managedSource),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// GetOrCreate returns the Source for the given URL, creating and connecting it if needed.
func (h *Hub) GetOrCreate(url string) *Source {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ms, ok := h.sources[url]; ok {
		return ms.source
	}

	src := NewSource(url)
	srcCtx, srcCancel := context.WithCancel(h.ctx)

	h.sources[url] = &managedSource{
		source: src,
		cancel: srcCancel,
	}

	go src.Connect(srcCtx)

	slog.Info("RTSP hub created source", "url", url)
	return src
}

// Get returns the Source for the given URL, or nil if it doesn't exist.
func (h *Hub) Get(url string) *Source {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ms, ok := h.sources[url]; ok {
		return ms.source
	}
	return nil
}

// Close disconnects all sources and shuts down the hub.
func (h *Hub) Close() {
	h.cancel()

	h.mu.Lock()
	defer h.mu.Unlock()

	for url, ms := range h.sources {
		ms.cancel()
		delete(h.sources, url)
	}

	slog.Info("RTSP hub closed")
}
