package graphql

import (
	"testing"

	"github.com/google/uuid"
)

func TestWebSocketStats(t *testing.T) {
	ResetWebSocketStats()

	for i := 0; i < 10; i++ {
		defaultWebSocketStats.AddActiveConnection(uuid.New())
	}

	for i := 0; i < 100; i++ {
		defaultWebSocketStats.AddClosedConnection(uuid.New())
	}

	stats := GetWebSocketStats()

	if got, expected := stats.TotalActiveConnections, 10; got != uint(expected) {
		t.Errorf("total active connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := stats.TotalClosedConnections, 100; got != uint(expected) {
		t.Errorf("total closed connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := len(defaultWebSocketStats.closedConnectionIDs), 100; got != expected {
		t.Errorf("total closed connection ids, expected: %d, got: %d", expected, got)
	}

	SetMaxClosedConnectionMetricCacheSize(10)

	if got, expected := stats.TotalClosedConnections, 100; got != uint(expected) {
		t.Errorf("total closed connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := len(defaultWebSocketStats.closedConnectionIDs), 10; got != expected {
		t.Errorf("total closed connection ids, expected: %d, got: %d", expected, got)
	}

	for i := 0; i < 10; i++ {
		defaultWebSocketStats.AddClosedConnection(uuid.New())
	}

	stats = GetWebSocketStats()

	if got, expected := stats.TotalClosedConnections, 110; got != uint(expected) {
		t.Errorf("total closed connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := len(defaultWebSocketStats.closedConnectionIDs), 10; got != expected {
		t.Errorf("total closed connection ids, expected: %d, got: %d", expected, got)
	}

	ResetWebSocketStats()
	stats = GetWebSocketStats()

	if got, expected := stats.TotalActiveConnections, 0; got != uint(expected) {
		t.Errorf("total active connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := stats.TotalClosedConnections, 0; got != uint(expected) {
		t.Errorf("total closed connections, expected: %d, got: %d", expected, got)
	}

	if got, expected := len(defaultWebSocketStats.closedConnectionIDs), 0; got != expected {
		t.Errorf("total closed connection ids, expected: %d, got: %d", expected, got)
	}
}
