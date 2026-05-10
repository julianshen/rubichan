package skills

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillEventBusSubscribeAndPublish(t *testing.T) {
	bus := NewSkillEventBus()

	var received []SkillEvent
	bus.Subscribe(func(evt SkillEvent) {
		received = append(received, evt)
	})

	bus.Publish(SkillEvent{
		Type:      EventSkillActivated,
		SkillName: "test-skill",
		State:     SkillStateActive,
	})

	require.Len(t, received, 1)
	assert.Equal(t, EventSkillActivated, received[0].Type)
	assert.Equal(t, "test-skill", received[0].SkillName)
	assert.Equal(t, SkillStateActive, received[0].State)
}

func TestSkillEventBusMultipleSubscribers(t *testing.T) {
	bus := NewSkillEventBus()

	var count1, count2 int
	bus.Subscribe(func(evt SkillEvent) { count1++ })
	bus.Subscribe(func(evt SkillEvent) { count2++ })

	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
	bus.Publish(SkillEvent{Type: EventSkillDeactivated, SkillName: "s2"})

	assert.Equal(t, 2, count1)
	assert.Equal(t, 2, count2)
}

func TestSkillEventBusUnsubscribe(t *testing.T) {
	bus := NewSkillEventBus()

	var count int
	handle := bus.Subscribe(func(evt SkillEvent) { count++ })

	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
	assert.Equal(t, 1, count)

	bus.Unsubscribe(handle)
	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s2"})
	assert.Equal(t, 1, count)
}

func TestSkillEventBusNoSubscribers(t *testing.T) {
	bus := NewSkillEventBus()
	// Should not panic.
	bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s1"})
}

func TestSkillEventBusConcurrentPublish(t *testing.T) {
	bus := NewSkillEventBus()

	var count int64
	var mu sync.Mutex
	bus.Subscribe(func(evt SkillEvent) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(SkillEvent{Type: EventSkillActivated, SkillName: "s"})
		}()
	}
	wg.Wait()

	mu.Lock()
	assert.Equal(t, int64(100), count)
	mu.Unlock()
}
