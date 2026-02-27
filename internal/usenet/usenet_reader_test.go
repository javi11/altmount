package usenet

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockSegmentLoader struct {
	mock.Mock
}

func (m *mockSegmentLoader) GetSegment(index int) (Segment, []string, bool) {
	args := m.Called(index)
	return args.Get(0).(Segment), args.Get(1).([]string), args.Bool(2)
}

func TestUsenetReader_NegativeCache(t *testing.T) {
	// This test is complex to set up because of the dependencies.
	// But we can verify that downloadSegmentWithRetry uses the negative cache.
	
	cache := NewNegativeCache(60 * time.Second)
	cache.Put("msg1")
	
	ur := &UsenetReader{
		negativeCache: cache,
		log:           slog.Default(),
	}
	
	ctx := context.Background()
	seg := &segment{Id: "msg1"}
	
	data, err := ur.downloadSegmentWithRetry(ctx, seg)
	assert.Nil(t, data)
	assert.True(t, errors.Is(err, nntppool.ErrArticleNotFound))
}
