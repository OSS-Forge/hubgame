package database

import (
	"context"
	"sync"
)

// Broker delivers events to topic subscribers with fan-out semantics.
type Broker struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan Event]struct{})}
}

func (b *Broker) Subscribe(ctx context.Context, topic string, buffer int) <-chan Event {
	ch := make(chan Event, buffer)

	b.mu.Lock()
	if _, ok := b.subs[topic]; !ok {
		b.subs[topic] = make(map[chan Event]struct{})
	}
	b.subs[topic][ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs[topic], ch)
		if len(b.subs[topic]) == 0 {
			delete(b.subs, topic)
		}
		b.mu.Unlock()
		close(ch)
	}()

	return ch
}

func (b *Broker) Publish(topic string, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subs[topic] {
		select {
		case ch <- event:
		default:
		}
	}
}
