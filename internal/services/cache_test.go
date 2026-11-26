package services

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Assures that concurrent identical fetches, such as a prefetch and a view load, share one request.
func TestFlightGroupSharesConcurrentFetches(t *testing.T) {
	var group flightGroup
	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			value, err := group.do("key", func() (any, error) {
				runs.Add(1)
				<-release
				return 42, nil
			})
			if err != nil || value.(int) != 42 {
				t.Errorf("do() = %v, %v", value, err)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := runs.Load(); got != 1 {
		t.Fatalf("fetch ran %d times, want 1", got)
	}
}
