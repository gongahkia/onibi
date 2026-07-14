package web

import "sync"

const defaultEventReplayLimit = 512

type Event struct {
	Seq     uint64
	Type    string
	Payload any
}

type EventReplay struct {
	Events   []Event
	Seq      uint64
	Snapshot bool
}

type EventBus struct {
	mu               sync.Mutex
	subscribers      map[int]chan Event
	nextSubscriberID int
	seq              uint64
	history          []Event
	replayLimit      int
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: map[int]chan Event{}, replayLimit: defaultEventReplayLimit}
}

func (b *EventBus) Subscribe() (<-chan Event, func()) {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
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

func (b *EventBus) SubscribeFrom(seq uint64) (EventReplay, <-chan Event, func()) {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return EventReplay{}, ch, func() {}
	}
	ch := make(chan Event, 64)
	b.mu.Lock()
	replay := b.replaySinceLocked(seq)
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
	return replay, ch, unsub
}

func (b *EventBus) Publish(ev Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	ev.Seq = b.seq
	b.history = append(b.history, ev)
	limit := b.replayLimit
	if limit <= 0 {
		limit = defaultEventReplayLimit
	}
	if len(b.history) > limit {
		b.history = append([]Event(nil), b.history[len(b.history)-limit:]...)
	}
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

func (b *EventBus) replaySinceLocked(seq uint64) EventReplay {
	replay := EventReplay{Seq: b.seq}
	if len(b.history) == 0 {
		if seq > b.seq {
			replay.Snapshot = true
		}
		return replay
	}
	earliest := b.history[0].Seq - 1
	if seq < earliest || seq > b.seq {
		replay.Snapshot = true
		return replay
	}
	for _, ev := range b.history {
		if ev.Seq > seq {
			replay.Events = append(replay.Events, ev)
		}
	}
	return replay
}
