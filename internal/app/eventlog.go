package app

import (
	"sync"
	"sync/atomic"
	"time"
)

type EventLog struct {
	mu     sync.RWMutex
	nextID atomic.Int64
	items  []Event
	limit  int
}

func NewEventLog(limit int) *EventLog {
	if limit < 50 {
		limit = 200
	}
	return &EventLog{limit: limit}
}

func (l *EventLog) Add(level, kind, message, peerID string) {
	item := Event{ID: l.nextID.Add(1), Time: time.Now(), Level: level, Kind: kind, Message: message, PeerID: peerID}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, item)
	if len(l.items) > l.limit {
		l.items = append([]Event(nil), l.items[len(l.items)-l.limit:]...)
	}
}

func (l *EventLog) List(limit int) []Event {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if limit <= 0 || limit > len(l.items) {
		limit = len(l.items)
	}
	start := len(l.items) - limit
	out := make([]Event, limit)
	for i := 0; i < limit; i++ {
		out[limit-1-i] = l.items[start+i]
	}
	return out
}
