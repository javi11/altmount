package database

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConnectionPool(t *testing.T) {
	tests := []struct {
		name         string
		workerCount  int
		wantMaxConns int
	}{
		{
			name:         "2 workers (minimum)",
			workerCount:  2,
			wantMaxConns: 6, // 2 + 4 buffer
		},
		{
			name:         "10 workers",
			workerCount:  10,
			wantMaxConns: 14, // 10 + 4 buffer
		},
		{
			name:         "28 workers (high load scenario)",
			workerCount:  28,
			wantMaxConns: 32, // 28 + 4 buffer
		},
		{
			name:         "50 workers (very high load)",
			workerCount:  50,
			wantMaxConns: 54, // 50 + 4 buffer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			db, err := sql.Open("sqlite3", ":memory:")
			require.NoError(t, err, "Failed to open in-memory database")
			defer db.Close()

			dbWrapper := &DB{conn: db}

			// Test: Update connection pool
			dbWrapper.UpdateConnectionPool(tt.workerCount)

			// Verify: MaxOpenConnections matches expected value
			stats := db.Stats()
			assert.Equal(t, tt.wantMaxConns, stats.MaxOpenConnections,
				"MaxOpenConnections should match formula: workers + 4")

			t.Logf("Worker count: %d → Max connections: %d", tt.workerCount, stats.MaxOpenConnections)
		})
	}
}

func TestUpdateConnectionPool_ZeroWorkers(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	dbWrapper := &DB{conn: db}

	// Test: Zero workers should default to minimum (2)
	dbWrapper.UpdateConnectionPool(0)

	// Verify: Defaults to minimum configuration
	stats := db.Stats()
	assert.Equal(t, 6, stats.MaxOpenConnections, "Should default to 2 workers + 4 buffer = 6")
}

func TestUpdateConnectionPool_NegativeWorkers(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	dbWrapper := &DB{conn: db}

	// Test: Negative workers should default to minimum (2)
	dbWrapper.UpdateConnectionPool(-5)

	// Verify: Handles negative values gracefully
	stats := db.Stats()
	assert.Equal(t, 6, stats.MaxOpenConnections, "Should default to 2 workers + 4 buffer = 6")
}

func TestUpdateConnectionPool_MultipleUpdates(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	dbWrapper := &DB{conn: db}

	// Test: Update pool multiple times (simulate worker count changes)
	testCases := []struct {
		workers  int
		expected int
	}{
		{2, 6},
		{10, 14},
		{5, 9},
		{28, 32},
		{1, 5}, // 1 worker + 4 buffer = 5
	}

	for _, tc := range testCases {
		dbWrapper.UpdateConnectionPool(tc.workers)
		stats := db.Stats()
		assert.Equal(t, tc.expected, stats.MaxOpenConnections,
			"After updating to %d workers, expected %d connections", tc.workers, tc.expected)
	}
}

func TestUpdateConnectionPool_ActualConnections(t *testing.T) {
	// Setup
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	setupQueueSchema(t, db)

	dbWrapper := &DB{conn: db}

	// Test: Set pool to support 5 workers
	dbWrapper.UpdateConnectionPool(5)

	// Verify: Can actually open concurrent connections up to the limit
	const numConnections = 9 // 5 + 4 buffer
	connections := make([]*sql.Conn, numConnections)

	for i := range numConnections {
		conn, err := db.Conn(context.Background())
		require.NoError(t, err, "Should be able to open connection %d", i+1)
		connections[i] = conn
	}

	// Check stats
	stats := db.Stats()
	assert.Equal(t, 9, stats.MaxOpenConnections)
	assert.LessOrEqual(t, stats.OpenConnections, 9, "Open connections should not exceed max")
	t.Logf("Open connections: %d, In use: %d", stats.OpenConnections, stats.InUse)

	// Cleanup
	for _, conn := range connections {
		if conn != nil {
			conn.Close()
		}
	}
}
