package usenet

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFlightMapRefCounting(t *testing.T) {
	fm := newFlightMap()
	a1 := fm.acquire("x", 10)
	a2 := fm.acquire("x", 10)
	if a1 != a2 || fm.len() != 1 {
		t.Fatal("same id must share one buffer")
	}
	fm.release("x", a1)
	if fm.len() != 1 {
		t.Fatal("still referenced")
	}
	fm.release("x", a2)
	if fm.len() != 0 {
		t.Fatal("must be forgotten once unreferenced")
	}
}

func TestArticleBufLeadAndWait(t *testing.T) {
	a := newArticleBuf(4)
	if !a.claimLead() || a.claimLead() {
		t.Fatal("claimLead must be exclusive")
	}
	if err := a.waitDone(timeoutCtx(t, 20*time.Millisecond)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitDone with a leader must block: %v", err)
	}
	a.releaseLead()
	if err := a.waitDone(context.Background()); !errors.Is(err, errNoLeader) {
		t.Fatalf("leaderless article must report errNoLeader: %v", err)
	}
	if !a.claimLead() {
		t.Fatal("lead must be re-claimable")
	}
	w := a.attemptWriter()
	_, _ = w.Write([]byte{1, 2, 3, 4})
	a.finish(w)
	if err := a.waitDone(context.Background()); err != nil {
		t.Fatalf("finished article: %v", err)
	}
	if a.claimLead() {
		t.Fatal("a finished article has no lead to claim")
	}
}

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
