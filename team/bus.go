package team

import (
	"fmt"
	"sync"
)

// MessageBus routes messages between team agents in-memory.
type MessageBus struct {
	mu       sync.RWMutex
	channels map[string]chan TeamMessage
}

// NewMessageBus creates a new message bus.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		channels: make(map[string]chan TeamMessage),
	}
}

// Register creates a buffered channel for the named agent and returns the receive end.
func (b *MessageBus) Register(name string) <-chan TeamMessage {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan TeamMessage, 64)
	b.channels[name] = ch
	return ch
}

// Unregister closes and removes the channel for the named agent.
func (b *MessageBus) Unregister(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.channels[name]; ok {
		close(ch)
		delete(b.channels, name)
	}
}

// Send routes a message to msg.Recipient.
func (b *MessageBus) Send(msg TeamMessage) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ch, ok := b.channels[msg.Recipient]
	if !ok {
		return fmt.Errorf("agent %q not registered", msg.Recipient)
	}

	select {
	case ch <- msg:
	default:
		return fmt.Errorf("inbox full for agent %q", msg.Recipient)
	}
	return nil
}

// Broadcast sends a message to all registered agents except the sender.
func (b *MessageBus) Broadcast(from string, msg TeamMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for name, ch := range b.channels {
		if name == from {
			continue
		}
		m := msg
		m.Recipient = name
		select {
		case ch <- m:
		default:
			// drop if full
		}
	}
}

// Members returns the names of all registered agents.
func (b *MessageBus) Members() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	names := make([]string, 0, len(b.channels))
	for name := range b.channels {
		names = append(names, name)
	}
	return names
}
