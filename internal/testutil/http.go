package testutil

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

type Server struct {
	URL string
	id  string
}

type memoryTransport struct{}

type memoryResponseWriter struct {
	header      http.Header
	request     *http.Request
	body        *memoryBody
	responseCh  chan *http.Response
	wroteHeader bool
	closeOnce   sync.Once
	mu          sync.Mutex
}

type memoryBody struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buffer bytes.Buffer
	closed bool
	err    error
}

var (
	registerTransportOnce sync.Once
	serverSeq             atomic.Uint64
	serverHandlers        sync.Map
)

// NewServer creates an in-memory HTTP server reachable via a mem:// URL.
// It avoids opening local listeners, which keeps tests hermetic in sandboxes.
func NewServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	registerMemoryTransport()

	id := strconv.FormatUint(serverSeq.Add(1), 10)
	serverHandlers.Store(id, handler)
	return &Server{
		URL: "mem://" + id,
		id:  id,
	}
}

func (s *Server) Close() {
	serverHandlers.Delete(s.id)
}

func (s *Server) Client() *http.Client {
	return http.DefaultClient
}

func registerMemoryTransport() {
	registerTransportOnce.Do(func() {
		http.DefaultTransport.(*http.Transport).RegisterProtocol("mem", memoryTransport{})
	})
}

func (memoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	handlerValue, ok := serverHandlers.Load(req.URL.Host)
	if !ok {
		return nil, fmt.Errorf("memory server %q not found", req.URL.Host)
	}
	handler := handlerValue.(http.Handler)

	body := newMemoryBody()
	writer := &memoryResponseWriter{
		header:     make(http.Header),
		request:    req,
		body:       body,
		responseCh: make(chan *http.Response, 1),
	}

	if done := req.Context().Done(); done != nil {
		go func() {
			<-done
			writer.closeBody(req.Context().Err())
		}()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				writer.closeBody(fmt.Errorf("handler panic: %v", r))
				return
			}
			if !writer.hasWrittenHeader() {
				writer.WriteHeader(http.StatusOK)
			}
			writer.closeBody(nil)
		}()

		handler.ServeHTTP(writer, req)
	}()

	select {
	case resp := <-writer.responseCh:
		return resp, nil
	case <-req.Context().Done():
		writer.closeBody(req.Context().Err())
		return nil, req.Context().Err()
	}
}

func (w *memoryResponseWriter) Header() http.Header {
	return w.header
}

func (w *memoryResponseWriter) Write(data []byte) (int, error) {
	if !w.hasWrittenHeader() {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *memoryResponseWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.responseCh <- &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     w.header.Clone(),
		Body:       w.body,
		Request:    w.request,
	}
}

func (w *memoryResponseWriter) Flush() {}

func (w *memoryResponseWriter) hasWrittenHeader() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wroteHeader
}

func (w *memoryResponseWriter) closeBody(err error) {
	w.closeOnce.Do(func() {
		if err != nil {
			_ = w.body.CloseWithError(err)
			return
		}
		_ = w.body.Close()
	})
}

func newMemoryBody() *memoryBody {
	body := &memoryBody{}
	body.cond = sync.NewCond(&body.mu)
	return body
}

func (b *memoryBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for b.buffer.Len() == 0 && !b.closed {
		b.cond.Wait()
	}
	if b.buffer.Len() > 0 {
		return b.buffer.Read(p)
	}
	if b.err != nil {
		return 0, b.err
	}
	return 0, io.EOF
}

func (b *memoryBody) Close() error {
	return b.CloseWithError(nil)
}

func (b *memoryBody) CloseWithError(err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.err = err
	b.cond.Broadcast()
	return nil
}

func (b *memoryBody) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		if b.err != nil {
			return 0, b.err
		}
		return 0, io.ErrClosedPipe
	}
	n, err := b.buffer.Write(p)
	b.cond.Broadcast()
	return n, err
}
