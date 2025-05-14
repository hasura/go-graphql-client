package graphql

import (
	"sync"

	"github.com/google/uuid"
)

// Closed connection IDs are cached to make sure the accuracy of the gauge metric when there are duplicated closed called.
var maxClosedConnectionCacheSize int = 100

var defaultWebSocketStats = websocketStats{
	activeConnectionIDs: map[uuid.UUID]bool{},
	closedConnectionIDs: make([]uuid.UUID, 0, maxClosedConnectionCacheSize),
}

// GetWebSocketStats gets the websocket stats.
func GetWebSocketStats() WebSocketStats {
	return defaultWebSocketStats.GetStats()
}

// ResetWebSocketStats reset the websocket stats.
func ResetWebSocketStats() {
	defaultWebSocketStats.Reset()
}

// WebSocketStats hold statistic data of WebSocket connections for subscription.
type WebSocketStats struct {
	TotalActiveConnections uint
	TotalClosedConnections uint
	ActiveConnectionIDs    []uuid.UUID
}

type websocketStats struct {
	sync                   sync.Mutex
	activeConnectionIDs    map[uuid.UUID]bool
	closedConnectionIDs    []uuid.UUID
	totalClosedConnections uint
}

// AddActiveConnection adds an active connection id to the list.
func (ws *websocketStats) AddActiveConnection(id uuid.UUID) {
	ws.sync.Lock()
	defer ws.sync.Unlock()

	ws.activeConnectionIDs[id] = true
}

// AddClosedConnection adds an dead connection id to the list.
func (ws *websocketStats) AddClosedConnection(id uuid.UUID) {
	ws.sync.Lock()
	defer ws.sync.Unlock()
	delete(ws.activeConnectionIDs, id)

	for _, item := range ws.closedConnectionIDs {
		// do not increase if the connection id already exists in the queue
		if item == id {
			return
		}
	}

	ws.totalClosedConnections++
	if len(ws.closedConnectionIDs) < maxClosedConnectionCacheSize {
		ws.closedConnectionIDs = append(ws.closedConnectionIDs, id)

		return
	}

	for i := 1; i < maxClosedConnectionCacheSize; i++ {
		ws.closedConnectionIDs[i-1] = ws.closedConnectionIDs[i]
	}

	ws.closedConnectionIDs[maxClosedConnectionCacheSize-1] = id
}

// Reset the websocket stats.
func (ws *websocketStats) Reset() {
	ws.sync.Lock()
	defer ws.sync.Unlock()

	ws.activeConnectionIDs = map[uuid.UUID]bool{}
	ws.closedConnectionIDs = make([]uuid.UUID, 0, maxClosedConnectionCacheSize)
	ws.totalClosedConnections = 0
}

// GetStats gets the websocket stats.
func (ws *websocketStats) GetStats() WebSocketStats {
	ws.sync.Lock()
	defer ws.sync.Unlock()

	activeIDs := make([]uuid.UUID, 0, len(ws.activeConnectionIDs))
	for id := range ws.activeConnectionIDs {
		activeIDs = append(activeIDs, id)
	}

	return WebSocketStats{
		ActiveConnectionIDs:    activeIDs,
		TotalActiveConnections: uint(len(ws.activeConnectionIDs)),
		TotalClosedConnections: ws.totalClosedConnections,
	}
}

// SetMaxClosedConnectionMetricCacheSize sets the max cache size of closed connections metrics.
func SetMaxClosedConnectionMetricCacheSize(size uint) {
	maxClosedConnectionCacheSize = int(size)

	if len(defaultWebSocketStats.closedConnectionIDs) <= maxClosedConnectionCacheSize {
		return
	}

	defaultWebSocketStats.sync.Lock()
	defer defaultWebSocketStats.sync.Unlock()

	newSlice := make([]uuid.UUID, maxClosedConnectionCacheSize)
	startIndex := len(defaultWebSocketStats.closedConnectionIDs) - maxClosedConnectionCacheSize

	for i := 0; i < maxClosedConnectionCacheSize; i++ {
		newSlice[i] = defaultWebSocketStats.closedConnectionIDs[startIndex+i]
	}

	defaultWebSocketStats.closedConnectionIDs = newSlice
}
