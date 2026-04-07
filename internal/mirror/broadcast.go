package mirror

import "sync"

// Broadcast is a multi-listener signaling mechanism.
// Calling Notify wakes all goroutines currently blocked on Wait.
type Broadcast struct {
	mu sync.Mutex
	ch chan struct{}
}

// NewBroadcast creates a ready-to-use Broadcast.
func NewBroadcast() *Broadcast {
	return &Broadcast{ch: make(chan struct{})}
}

// Wait returns a channel that will be closed on the next Notify call.
func (b *Broadcast) Wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

// Notify wakes all goroutines currently waiting.
func (b *Broadcast) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.ch)
	b.ch = make(chan struct{})
}
