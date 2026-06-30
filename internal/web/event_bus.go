package web

import "sync"

type Event struct {
	Type    string
	Payload any
}

type EventBus struct {
	mu               sync.Mutex
	subscribers      map[int]chan Event
	nextSubscriberID int
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: map[int]chan Event{}}
}

func (b *EventBus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.subscribers[id] = ch
	b.mu.Unlock()
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
	return ch, unsub
}

func (b *EventBus) Publish(ev Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
