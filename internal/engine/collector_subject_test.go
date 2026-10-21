package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/omalloc/probecube-agent/internal/messaging"
)

// fakeSubscriber records which subjects the collector subscribes to so the test
// can assert on the exact subject set.
type fakeSubscriber struct {
	mu       sync.Mutex
	subjects []string
}

func (f *fakeSubscriber) Subscribe(_ context.Context, subject string, _ messaging.Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjects = append(f.subjects, subject)
	return nil
}

func (f *fakeSubscriber) Unsubscribe(context.Context, string) {}

func (f *fakeSubscriber) HasReady() bool { return true }

func (f *fakeSubscriber) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.subjects))
	copy(out, f.subjects)
	return out
}

// TestSubscribes_OnlyNodeSpecificSubject guards the subject contract the auth
// callout service grants. Subscribing to the shared broadcast subject would both
// deliver other nodes' tasks to this agent and be rejected by the broker once
// subject-level permissions are enforced.
func TestSubscribes_OnlyNodeSpecificSubject(t *testing.T) {
	sub := &fakeSubscriber{}
	c := &Collector{subscriber: sub, nodeSN: "SN-TEST"}

	if err := c.subscribes(context.Background()); err != nil {
		t.Fatalf("subscribes() = %v, want nil", err)
	}

	got := sub.recorded()
	want := DialSysProbeTaskSubject + ".SN-TEST"

	if len(got) != 1 {
		t.Fatalf("subscribed to %d subjects %v, want exactly 1 (%q)", len(got), got, want)
	}
	if got[0] != want {
		t.Fatalf("subscribed to %q, want %q", got[0], want)
	}
	for _, subject := range got {
		if subject == DialSysProbeTaskSubject {
			t.Fatalf("subscribed to the broadcast subject %q; it must be removed", subject)
		}
	}
}
