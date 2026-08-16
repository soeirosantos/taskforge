package worker

import (
	"context"
	"sync"
	"testing"
)

func TestRegistryCancelSignalsRegisteredExecution(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	ctx, release := reg.Register(context.Background(), "job-1")
	defer release()

	if !reg.has("job-1") {
		t.Fatal("registered job is not present in the registry")
	}
	if ctx.Err() != nil {
		t.Fatalf("freshly registered context is already done: %v", ctx.Err())
	}

	if !reg.Cancel("job-1") {
		t.Fatal("Cancel reported no entry for a registered job")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("execution context was not cancelled by Cancel")
	}
}

func TestRegistryCancelUnknownJob(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if reg.Cancel("missing") {
		t.Fatal("Cancel reported an entry for an unregistered job")
	}
}

func TestRegistryReleaseRemovesEntryAndCancels(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	ctx, release := reg.Register(context.Background(), "job-1")

	release()
	if reg.has("job-1") {
		t.Fatal("entry still present after release")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("release did not cancel the execution context")
	}

	// Releasing twice must be safe: the pool defers release and may also have
	// released explicitly.
	release()
	if reg.size() != 0 {
		t.Fatalf("registry size after double release = %d, want 0", reg.size())
	}
}

func TestRegistryReleaseLeavesLaterEntryAlone(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	_, releaseFirst := reg.Register(context.Background(), "job-1")
	secondCtx, releaseSecond := reg.Register(context.Background(), "job-1")
	defer releaseSecond()

	releaseFirst()

	if !reg.has("job-1") {
		t.Fatal("releasing a superseded entry removed the live one")
	}
	if secondCtx.Err() != nil {
		t.Fatal("releasing a superseded entry cancelled the live execution")
	}
	if !reg.Cancel("job-1") {
		t.Fatal("live entry is not cancellable")
	}
}

func TestRegistryCancelAll(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	first, releaseFirst := reg.Register(context.Background(), "job-1")
	defer releaseFirst()
	second, releaseSecond := reg.Register(context.Background(), "job-2")
	defer releaseSecond()

	reg.CancelAll()

	for name, ctx := range map[string]context.Context{"job-1": first, "job-2": second} {
		select {
		case <-ctx.Done():
		default:
			t.Fatalf("%s was not cancelled by CancelAll", name)
		}
	}
}

func TestRegistryParentCancellationPropagates(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	ctx, release := reg.Register(parent, "job-1")
	defer release()

	cancelParent()
	<-ctx.Done()
}

// TestRegistryConcurrentUse is the race-detector exercise for the registry
// itself: registrations, releases, and cancellations from many goroutines at
// once must not race and must leave the map empty.
func TestRegistryConcurrentUse(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	const goroutines = 16

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i%4))
			for n := 0; n < 200; n++ {
				// Ids collide deliberately: several goroutines share one id so
				// that the release identity check is exercised. No goroutine
				// may block on its own context being cancelled, because a
				// colliding Register may have replaced its entry.
				ctx, release := reg.Register(context.Background(), id)
				reg.Cancel(id)
				reg.CancelAll()
				release()
				if ctx.Err() == nil {
					t.Errorf("context still live after release")
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if reg.size() != 0 {
		t.Fatalf("registry size after concurrent use = %d, want 0", reg.size())
	}
}
