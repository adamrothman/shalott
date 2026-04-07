package mirror_test

import (
	"sync"
	"testing"
	"time"

	"github.com/adamrothman/shalott/internal/mirror"
)

func TestBroadcast_WakesAllListeners(t *testing.T) {
	b := mirror.NewBroadcast()

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)

	for range n {
		go func() {
			defer wg.Done()
			<-b.Wait()
		}()
	}

	// Give goroutines time to block on Wait
	time.Sleep(10 * time.Millisecond)
	b.Notify()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all listeners woke up
	case <-time.After(time.Second):
		t.Fatal("not all listeners woke up after Notify")
	}
}

func TestBroadcast_MultipleNotifies(t *testing.T) {
	b := mirror.NewBroadcast()

	for i := range 3 {
		ch := b.Wait()
		b.Notify()

		select {
		case <-ch:
			// expected
		default:
			t.Fatalf("iteration %d: Wait channel was not closed by Notify", i)
		}

		// Next Wait should return a fresh, open channel
		select {
		case <-b.Wait():
			t.Fatalf("iteration %d: fresh channel should not be closed", i)
		default:
			// expected
		}
	}
}

func TestBroadcast_WaitBeforeNotify(t *testing.T) {
	b := mirror.NewBroadcast()

	// Wait should not be immediately ready
	select {
	case <-b.Wait():
		t.Fatal("Wait channel should block before Notify is called")
	default:
		// expected
	}
}
