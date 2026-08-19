package par2repair

import (
	"bytes"
	"sync"
	"testing"
)

func TestPatchStoreRoundTrip(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	payload := bytes.Repeat([]byte{0xCD}, 1500)

	if _, ok := p.Get("<a@test>"); ok {
		t.Fatal("unexpected hit before Put")
	}
	if p.Has("<a@test>") {
		t.Fatal("unexpected Has before Put")
	}
	if err := p.Put("<a@test>", payload); err != nil {
		t.Fatal(err)
	}
	got, ok := p.Get("<a@test>")
	if !ok || !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
	if !p.Has("<a@test>") {
		t.Fatal("Has = false after Put")
	}
	// Distinct key stays a miss.
	if _, ok := p.Get("<b@test>"); ok {
		t.Fatal("unexpected hit for other key")
	}
}

func TestPatchStoreEmptyMessageID(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	if err := p.Put("", []byte("x")); err == nil {
		t.Fatal("want error for empty message ID")
	}
}

func TestPatchStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	if err := NewPatchStore(dir).Put("<a@test>", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, ok := NewPatchStore(dir).Get("<a@test>")
	if !ok || string(got) != "hello" {
		t.Fatal("patch not visible to a fresh store over the same dir")
	}
}

// A concurrent reader must always see a complete payload (one of the two
// alternating writes) or a miss — never a partial file.
func TestPatchStoreAtomicPut(t *testing.T) {
	p := NewPatchStore(t.TempDir())
	small := bytes.Repeat([]byte{0x01}, 100)
	large := bytes.Repeat([]byte{0x02}, 100_000)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, ok := p.Get("<hot@test>")
			if !ok {
				continue
			}
			if !bytes.Equal(got, small) && !bytes.Equal(got, large) {
				t.Errorf("partial payload observed: %d bytes", len(got))
				return
			}
		}
	}()

	for i := range 100 {
		payload := small
		if i%2 == 1 {
			payload = large
		}
		if err := p.Put("<hot@test>", payload); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}
