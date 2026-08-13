package a2a

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/agent"
)

// maxRPCRequestBytes caps the size of a decoded JSON-RPC request body. The
// server accepts connections from any authenticated client, so an unbounded
// Decode(r.Body) would let a malicious or buggy caller exhaust server
// memory by streaming an unbounded body. 10MB is generous enough for real
// task messages while still being bounded.
const maxRPCRequestBytes = 10 << 20

// Server answers A2A protocol requests by driving a real agent.Agent turn
// per task.
type Server struct {
	agent       *agent.Agent
	tasks       TaskStore
	card        AgentCard
	requireAuth bool
	apiKey      string

	// cancels holds the context.CancelFunc for each task's in-flight
	// runTurn call, keyed by task ID, so tasks/cancel can actually
	// interrupt the running turn rather than only marking the task
	// canceled in the store. Entries are added in handleMessageSend /
	// handleMessageStream just before starting the turn and removed once
	// the turn returns (success, failure, or cancellation).
	cancels sync.Map // map[string]context.CancelFunc
}

// NewServer creates a Server. When requireAuth is true, every JSON-RPC
// request (not the Agent Card fetch) must carry a matching
// "Authorization: Bearer <apiKey>" header.
func NewServer(a *agent.Agent, tasks TaskStore, card AgentCard, requireAuth bool, apiKey string) *Server {
	return &Server{agent: a, tasks: tasks, card: card, requireAuth: requireAuth, apiKey: apiKey}
}

// Handler returns the HTTP handler serving the Agent Card and the
// JSON-RPC endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", s.handleAgentCard)
	mux.HandleFunc("/", s.handleRPC)
	return mux
}

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if s.requireAuth && !s.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRPCRequestBytes)

	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, 0, -32700, "parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "message/send":
		s.handleMessageSend(w, r, req)
	case "message/stream":
		s.handleMessageStream(w, r, req)
	case "tasks/get":
		s.handleTasksGet(w, r, req)
	case "tasks/cancel":
		s.handleTasksCancel(w, r, req)
	default:
		s.writeError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	if s.apiKey == "" {
		// An empty configured key must never authorize anything, even a
		// client sending exactly "Bearer " (empty token) — otherwise a
		// Server constructed with requireAuth=true and apiKey="" silently
		// accepts every request.
		return false
	}
	got := r.Header.Get("Authorization")
	expected := "Bearer " + s.apiKey
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

type taskIDParams struct {
	ID string `json:"id"`
}

func (s *Server) handleTasksGet(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params taskIDParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	task, err := s.tasks.Get(r.Context(), params.ID)
	if err != nil {
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}
	result, _ := json.Marshal(task)
	s.writeResult(w, req.ID, result)
}

func (s *Server) handleTasksCancel(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params taskIDParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	ctx := r.Context()
	task, err := s.tasks.Get(ctx, params.ID)
	if err != nil {
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}
	task.State = TaskCanceled
	// Persist TaskCanceled before invoking the turn's CancelFunc: this
	// ordering minimizes (though, per the check-then-update race noted on
	// s.finishTurn, cannot eliminate) the window in which the send
	// handler's own "is this task already canceled?" check reads stale
	// TaskWorking state and overwrites this cancellation.
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}
	s.cancelRunningTurn(task.ID)
	result, _ := json.Marshal(task)
	s.writeResult(w, req.ID, result)
}

// cancelRunningTurn interrupts the in-flight runTurn call for taskID, if
// one is currently tracked. It is a no-op if the task has no running turn
// (already finished, or was never started via message/send).
func (s *Server) cancelRunningTurn(taskID string) {
	if cancel, ok := s.cancels.Load(taskID); ok {
		if cancelFn, ok := cancel.(context.CancelFunc); ok {
			cancelFn()
		}
	}
}

// trackTurn derives a cancelable context for taskID's runTurn call and
// registers it in s.cancels so a concurrent tasks/cancel can interrupt it.
// The returned done func must be deferred by the caller: it cancels the
// context (releasing its resources even on the normal-completion path) and
// removes the map entry so it cannot leak or be canceled twice.
func (s *Server) trackTurn(ctx context.Context, taskID string) (turnCtx context.Context, done func()) {
	turnCtx, cancel := context.WithCancel(ctx)
	s.cancels.Store(taskID, cancel)
	return turnCtx, func() {
		cancel()
		s.cancels.Delete(taskID)
	}
}

// finishTurn decides what a handler should persist and return after
// runTurn completes for task. If the store already holds TaskCanceled for
// this task's ID — meaning tasks/cancel raced with the turn finishing on
// its own — the canceled task from the store is returned as-is and the
// caller must not overwrite it with task's locally-computed terminal
// state (TaskCompleted/TaskFailed).
//
// Known limitation: this is a check-then-update against ctx, the same ctx
// tasks/cancel writes through, and is not atomic against a concurrent
// Update landing between the Get here and the caller's own Update. A
// fully race-free version would need the TaskStore to support a
// conditional/compare-and-swap update, which is out of scope for this fix
// — the store interface has no such primitive today. This closes the
// overwhelming majority of cases (in particular, the ordering exercised by
// this package's own cancellation test) but is not an airtight guarantee
// under arbitrary interleaving.
func (s *Server) finishTurn(ctx context.Context, task Task) (result Task, alreadyCanceled bool) {
	if current, err := s.tasks.Get(ctx, task.ID); err == nil && current.State == TaskCanceled {
		return current, true
	}
	return task, false
}

type sendParams struct {
	Message Message `json:"message"`
}

func (s *Server) handleMessageSend(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params sendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	task := Task{ID: newTaskID(), ContextID: newTaskID(), State: TaskSubmitted}
	ctx := r.Context()
	if err := s.tasks.Create(ctx, task); err != nil {
		log.Printf("warning: a2a server task store create failed for task %s: %v", task.ID, err)
	}

	task.State = TaskWorking
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}

	turnCtx, done := s.trackTurn(ctx, task.ID)
	task = s.runTurn(turnCtx, task, messageText(params.Message))
	done()

	task, alreadyCanceled := s.finishTurn(ctx, task)
	if !alreadyCanceled {
		if err := s.tasks.Update(ctx, task); err != nil {
			log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
		}
	}

	result, _ := json.Marshal(task)
	s.writeResult(w, req.ID, result)
}

func (s *Server) handleMessageStream(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params sendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, req.ID, -32603, "streaming unsupported by this response writer")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ctx := r.Context()
	task := Task{ID: newTaskID(), ContextID: newTaskID(), State: TaskSubmitted}
	if err := s.tasks.Create(ctx, task); err != nil {
		log.Printf("warning: a2a server task store create failed for task %s: %v", task.ID, err)
	}
	writeSSETask(w, flusher, task)

	task.State = TaskWorking
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}
	writeSSETask(w, flusher, task)

	turnCtx, done := s.trackTurn(ctx, task.ID)
	task = s.runTurn(turnCtx, task, messageText(params.Message))
	done()

	task, alreadyCanceled := s.finishTurn(ctx, task)
	if !alreadyCanceled {
		if err := s.tasks.Update(ctx, task); err != nil {
			log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
		}
	}
	writeSSETask(w, flusher, task)
}

func writeSSETask(w http.ResponseWriter, flusher http.Flusher, task Task) {
	data, err := json.Marshal(task)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
	flusher.Flush()
}

// runTurn drives task through a real agent turn, mirroring
// internal/agent/subagent.go's Spawn: accumulate text_delta into the
// final Artifact, watch for error/done. A panic inside Turn's own
// execution is recovered here so a single bad task can never take the
// listener down.
func (s *Server) runTurn(ctx context.Context, task Task, prompt string) (result Task) {
	defer func() {
		if r := recover(); r != nil {
			result = task
			result.State = TaskFailed
			result.Error = fmt.Sprintf("panic: %v", r)
		}
	}()

	eventCh, err := s.agent.Turn(ctx, prompt)
	if err != nil {
		task.State = TaskFailed
		task.Error = err.Error()
		return task
	}

	var out strings.Builder
	for event := range eventCh {
		switch event.Type {
		case "text_delta":
			out.WriteString(event.Text)
		case "error":
			task.State = TaskFailed
			if event.Error != nil {
				task.Error = event.Error.Error()
			}
		}
	}

	if task.State == TaskFailed {
		return task
	}

	task.State = TaskCompleted
	task.Artifacts = []Artifact{{Name: "result", Parts: []Part{{Kind: "text", Text: out.String()}}}}
	return task
}

func messageText(m Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func (s *Server) writeError(w http.ResponseWriter, id int64, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}})
}

// writeResult sets Content-Type like writeError already does and encodes a
// successful JSON-RPC response. Shared by handleMessageSend, handleTasksGet
// and handleTasksCancel so all three respond consistently.
func (s *Server) writeResult(w http.ResponseWriter, id int64, result json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: id, Result: result})
}

// newTaskID generates a UUID-based task ID. IDs are persisted in the shared
// a2a_tasks table where (role, id) is the primary key, so a process-local
// sequential counter would collide with previously-persisted tasks after a
// restart ("task-1" already existing from a prior run). A UUID has no
// process-local state to reset.
func newTaskID() string {
	return "task-" + uuid.NewString()
}
