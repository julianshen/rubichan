package headless

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeadlessACPClientTimeoutDefault(t *testing.T) {
	client := &ACPClient{
		nextID:  1,
		timeout: 30,
	}

	assert.Equal(t, 30, client.Timeout())
}

func TestHeadlessACPClientSetTimeout(t *testing.T) {
	client := &ACPClient{
		nextID:  1,
		timeout: 30,
	}

	client.SetTimeout(60)
	assert.Equal(t, 60, client.Timeout())

	client.SetTimeout(5)
	assert.Equal(t, 5, client.Timeout())
}

func TestHeadlessACPClientGetNextID(t *testing.T) {
	client := &ACPClient{
		nextID:  1,
		timeout: 30,
	}

	id1 := client.getNextID()
	id2 := client.getNextID()
	id3 := client.getNextID()

	assert.Equal(t, int64(1), id1)
	assert.Equal(t, int64(2), id2)
	assert.Equal(t, int64(3), id3)
}

func TestHeadlessACPClientGetNextIDConcurrent(t *testing.T) {
	client := &ACPClient{
		nextID:  1,
		timeout: 30,
	}

	const goroutines = 10
	const idsPerGoroutine = 100

	var wg sync.WaitGroup
	idCh := make(chan int64, goroutines*idsPerGoroutine)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				idCh <- client.getNextID()
			}
		}()
	}

	wg.Wait()
	close(idCh)

	seen := make(map[int64]bool)
	for id := range idCh {
		assert.False(t, seen[id], "duplicate ID: %d", id)
		seen[id] = true
	}
	assert.Len(t, seen, goroutines*idsPerGoroutine)
}

func TestHeadlessACPClientCloseNilDispatcher(t *testing.T) {
	client := &ACPClient{
		nextID:  1,
		timeout: 30,
	}

	err := client.Close()
	assert.NoError(t, err)
}
