package auth

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithProfileLockRunsAndPropagates(t *testing.T) {
	withTempDir(t)

	ran := false
	if err := withProfileLock("work", func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn was not run under the lock")
	}

	sentinel := errors.New("boom")
	if err := withProfileLock("work", func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the fn's error propagated", err)
	}
}

func TestWithProfileLockSerializes(t *testing.T) {
	withTempDir(t)

	var inCritical, overlaps, completed int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withProfileLock("work", func() error {
				if atomic.AddInt32(&inCritical, 1) != 1 {
					atomic.AddInt32(&overlaps, 1) // another goroutine was inside at the same time
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt32(&inCritical, -1)
				atomic.AddInt32(&completed, 1)
				return nil
			})
		}()
	}
	wg.Wait()

	if overlaps != 0 {
		t.Fatalf("lock allowed %d overlapping critical sections", overlaps)
	}
	if completed != 4 {
		t.Fatalf("completed = %d, want 4", completed)
	}
}
