package vfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRanges_Insert_Basic(t *testing.T) {
	r := NewRanges()
	r.Insert(10, 20)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(10), r.Size())
	assert.True(t, r.Present(10, 20))
	assert.False(t, r.Present(0, 10))
}

func TestRanges_Insert_Coalesce(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 10)
	r.Insert(10, 20)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(20), r.Size())
	assert.True(t, r.Present(0, 20))
}

func TestRanges_Insert_Overlap(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 15)
	r.Insert(10, 25)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(25), r.Size())
	assert.True(t, r.Present(0, 25))
}

func TestRanges_Insert_Gap(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 10)
	r.Insert(20, 30)
	assert.Equal(t, 2, r.Count())
	assert.Equal(t, int64(20), r.Size())
	assert.False(t, r.Present(0, 30))
	assert.True(t, r.Present(0, 10))
	assert.True(t, r.Present(20, 30))
}

func TestRanges_Insert_FillGap(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 10)
	r.Insert(20, 30)
	r.Insert(10, 20)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(30), r.Size())
	assert.True(t, r.Present(0, 30))
}

func TestRanges_Insert_SubsetNoOp(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 30)
	r.Insert(5, 15)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(30), r.Size())
}

func TestRanges_Insert_Superset(t *testing.T) {
	r := NewRanges()
	r.Insert(5, 10)
	r.Insert(15, 20)
	r.Insert(0, 30)
	assert.Equal(t, 1, r.Count())
	assert.Equal(t, int64(30), r.Size())
}

func TestRanges_Insert_Invalid(t *testing.T) {
	r := NewRanges()
	r.Insert(10, 5) // end < start
	assert.Equal(t, 0, r.Count())
	r.Insert(5, 5) // empty range
	assert.Equal(t, 0, r.Count())
}

func TestRanges_Present(t *testing.T) {
	r := NewRanges()
	r.Insert(10, 20)
	r.Insert(30, 40)

	assert.True(t, r.Present(10, 15))
	assert.True(t, r.Present(10, 20))
	assert.True(t, r.Present(30, 40))
	assert.False(t, r.Present(5, 15))
	assert.False(t, r.Present(15, 35))
	assert.False(t, r.Present(25, 28))
	assert.True(t, r.Present(5, 5)) // empty range always present
}

func TestRanges_FindMissing(t *testing.T) {
	r := NewRanges()
	r.Insert(10, 20)
	r.Insert(30, 40)

	missing := r.FindMissing(0, 50)
	assert.Equal(t, []Range{
		{Start: 0, End: 10},
		{Start: 20, End: 30},
		{Start: 40, End: 50},
	}, missing)
}

func TestRanges_FindMissing_FullyCovered(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 100)

	missing := r.FindMissing(10, 50)
	assert.Nil(t, missing)
}

func TestRanges_FindMissing_NonePresent(t *testing.T) {
	r := NewRanges()

	missing := r.FindMissing(0, 100)
	assert.Equal(t, []Range{{Start: 0, End: 100}}, missing)
}

func TestRanges_FindMissing_PartialOverlap(t *testing.T) {
	r := NewRanges()
	r.Insert(5, 15)

	missing := r.FindMissing(0, 10)
	assert.Equal(t, []Range{{Start: 0, End: 5}}, missing)
}

func TestRanges_Items_FromItems(t *testing.T) {
	r := NewRanges()
	r.Insert(0, 10)
	r.Insert(20, 30)

	items := r.Items()
	assert.Equal(t, 2, len(items))

	r2 := NewRanges()
	r2.FromItems(items)
	assert.Equal(t, r.Count(), r2.Count())
	assert.Equal(t, r.Size(), r2.Size())
	assert.True(t, r2.Present(0, 10))
	assert.True(t, r2.Present(20, 30))
}
