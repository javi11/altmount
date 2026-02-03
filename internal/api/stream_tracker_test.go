package api

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/stretchr/testify/assert"
)

func TestStreamTracker_GetAll_Grouping(t *testing.T) {
	tracker := NewStreamTracker()
	defer tracker.Stop()

	// Add 3 connections for the same file
	s1 := tracker.AddStream("/movies/movie.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 1000)
	s2 := tracker.AddStream("/movies/movie.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 1000)
	s3 := tracker.AddStream("/movies/movie.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 1000)

	// Add some bytes sent to each
	atomic.AddInt64(&s1.BytesSent, 100)
	atomic.AddInt64(&s2.BytesSent, 200)
	atomic.AddInt64(&s3.BytesSent, 300)

	// Add another file for same user
	s4 := tracker.AddStream("/movies/other.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 2000)
	atomic.AddInt64(&s4.BytesSent, 500)

	// Add same file for different user
	s5 := tracker.AddStream("/movies/movie.mkv", "WebDAV", "user2", "127.0.0.1", "TestAgent", 1000)
	atomic.AddInt64(&s5.BytesSent, 50)

	streams := tracker.GetAll()

	// Should have 3 aggregated streams:
	// 1. movie.mkv for user1 (aggregated from 3)
	// 2. other.mkv for user1
	// 3. movie.mkv for user2
	assert.Len(t, streams, 3)

	// Find the aggregated stream for movie.mkv user1
	var movieUser1 *nzbfilesystem.ActiveStream
	for _, s := range streams {
		if s.FilePath == "/movies/movie.mkv" && s.UserName == "user1" {
			movieUser1 = &s
			break
		}
	}

	assert.NotNil(t, movieUser1)
	assert.Equal(t, int64(600), movieUser1.BytesSent) // 100 + 200 + 300
	assert.Equal(t, int64(1000), movieUser1.TotalSize)
	assert.Equal(t, "/movies/movie.mkv|user1|WebDAV|127.0.0.1|TestAgent", movieUser1.ID)
}

func TestStreamTracker_GetAll_Sorting(t *testing.T) {
	tracker := NewStreamTracker()
	defer tracker.Stop()

	// Add an older stream
	s1 := tracker.AddStream("/old.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 1000)
	s1.StartedAt = time.Now().Add(-10 * time.Minute)

	// Add a newer stream
	s2 := tracker.AddStream("/new.mkv", "WebDAV", "user1", "127.0.0.1", "TestAgent", 1000)
	s2.StartedAt = time.Now().Add(-1 * time.Minute)

	streams := tracker.GetAll()

	assert.Len(t, streams, 2)
	assert.Equal(t, "/new.mkv", streams[0].FilePath)
	assert.Equal(t, "/old.mkv", streams[1].FilePath)
}
