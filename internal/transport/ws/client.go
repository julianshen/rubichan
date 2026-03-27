package ws

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const (
	// writeWait is the maximum duration to wait for a write to complete.
	writeWait = 10 * time.Second

	// pongWait is the maximum duration to wait for a pong response.
	pongWait = 60 * time.Second

	// pingPeriod is the interval between pings. Must be less than pongWait.
	pingPeriod = 50 * time.Second

	// sendBufferSize is the capacity of the outbound message channel.
	sendBufferSize = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	hub    *Hub
	conn   net.Conn
	send   chan []byte
	claims AuthClaims
	closed atomic.Bool
	once   sync.Once
}

// newClient creates a new client for the given connection.
func newClient(hub *Hub, conn net.Conn, claims AuthClaims) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
		claims: claims,
	}
}

// Send enqueues a message for delivery. Returns false if the client is closed.
func (c *Client) Send(data []byte) bool {
	if c.closed.Load() {
		return false
	}
	select {
	case c.send <- data:
		return true
	default:
		// Buffer full — close the slow client.
		c.close()
		return false
	}
}

// close shuts down the client connection. Safe to call multiple times.
func (c *Client) close() {
	c.once.Do(func() {
		c.closed.Store(true)
		close(c.send)
		c.conn.Close()
	})
}

// readPump reads messages from the WebSocket and dispatches to the hub.
// Blocks until the connection is closed or an error occurs.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregisterClient(c)
		c.close()
	}()

	for {
		msg, op, err := wsutil.ReadClientData(c.conn)
		if err != nil {
			return
		}
		if op != ws.OpText {
			continue
		}

		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			errPayload, _ := json.Marshal(ErrorPayload{
				Code:    "invalid_json",
				Message: fmt.Sprintf("failed to parse message: %v", err),
			})
			errEnv := Envelope{
				Type:      TypeError,
				Timestamp: time.Now().UTC(),
				Payload:   errPayload,
			}
			data, _ := json.Marshal(errEnv)
			c.Send(data)
			continue
		}

		c.hub.handleClientMessage(c, env)
	}
}

// writePump drains the send channel and writes messages to the WebSocket.
// Also handles periodic ping frames.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// Channel closed.
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpText, msg); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPing, nil); err != nil {
				return
			}
		}
	}
}
