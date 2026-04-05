package ws

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const (
	// writeWait is the maximum duration to wait for a write to complete.
	writeWait = 10 * time.Second

	// pongWait is the maximum duration to wait for a pong response.
	// readPump sets a read deadline based on this value.
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
	done   chan struct{} // signals shutdown to Send callers
	claims AuthClaims
	once   sync.Once
}

// newClient creates a new client for the given connection.
func newClient(hub *Hub, conn net.Conn, claims AuthClaims) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
		done:   make(chan struct{}),
		claims: claims,
	}
}

// sendError sends an error response to the client.
func (c *Client) sendError(code, message string) {
	errPayload, _ := json.Marshal(ErrorPayload{Code: code, Message: message})
	data, _ := json.Marshal(Envelope{
		Type:      TypeError,
		Timestamp: time.Now().UTC(),
		Payload:   errPayload,
	})
	c.Send(data)
}

// Send enqueues a message for delivery. Returns false if the client is closed
// or the send buffer is full (slow client).
// When the buffer fills, the client is closed to prevent memory buildup,
// and a warning is logged.
func (c *Client) Send(data []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- data:
		return true
	case <-c.done:
		return false
	default:
		// Buffer full — close the slow client to prevent unbounded memory growth.
		// The client's readPump defers cleanup on connection close.
		log.Printf("ws: closing slow client: send buffer is full (capacity=%d)", sendBufferSize)
		c.close()
		return false
	}
}

// close shuts down the client connection. Safe to call multiple times.
func (c *Client) close() {
	c.once.Do(func() {
		close(c.done)
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

	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// maxMessageSize limits inbound frames to prevent OOM from oversized payloads.
	const maxMessageSize = 1 << 20 // 1 MiB

	// Use a custom reader to intercept control frames. We do NOT use
	// wsutil.ControlFrameHandler here because it writes responses (close frames)
	// back to c.conn, which would race with writePump's concurrent writes.
	// Instead, readPump only reads; all writes go through writePump via c.send.
	reader := wsutil.NewReader(c.conn, ws.StateServerSide)
	reader.MaxFrameSize = maxMessageSize
	reader.OnIntermediate = func(hdr ws.Header, r io.Reader) error {
		// Reset deadline on any control frame (pong keeps connection alive).
		if hdr.OpCode == ws.OpPong {
			c.conn.SetReadDeadline(time.Now().Add(pongWait))
		}
		// Drain the frame payload without writing a response.
		// Note: RFC 6455 §5.5.1 requires echoing Close frames, but doing so
		// from readPump would race with writePump. The deferred c.close()
		// forcibly closes the TCP connection, which clients handle gracefully.
		// Close frames cause readPump to return on the next read error.
		_, err := io.Copy(io.Discard, r)
		return err
	}

	for {
		hdr, err := reader.NextFrame()
		if err != nil {
			return
		}
		if hdr.OpCode.IsControl() {
			if err := reader.OnIntermediate(hdr, reader); err != nil {
				return
			}
			continue
		}
		msg, err := io.ReadAll(reader)
		if err != nil {
			return
		}

		// Extend read deadline on any received data frame.
		c.conn.SetReadDeadline(time.Now().Add(pongWait))

		if hdr.OpCode != ws.OpText {
			continue
		}

		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			c.sendError("invalid_json", "invalid JSON message")
			continue
		}

		// Validate envelope structure (type must be known).
		if err := env.Validate(); err != nil {
			c.sendError("invalid_envelope", err.Error())
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
		case msg := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpText, msg); err != nil {
				return
			}

		case <-c.done:
			return

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPing, nil); err != nil {
				return
			}
		}
	}
}
