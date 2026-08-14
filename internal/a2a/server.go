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

	// taskLocks holds one *sync.Mutex per task ID ever seen by
	// handleTasksCancel or finishTurn, serializing each task's
	// "read current stored state, decide, write terminal state" sequence
	// between those two call sites (see lockFor). This closes the
	// check-then-update race documented on finishTurn's history.
	//
	// Deliberately NOT cleaned up. Entries here are not bounded the way
	// s.cancels' are (s.cancels only holds entries for turns currently
	// in flight; taskLocks would hold one entry per task ID ever
	// processed, for the life of the process — a slow, unbounded memory
	// leak of small (~48 byte) *sync.Mutex values). A naive "delete once
	// terminal" was considered and rejected: deleting a sync.Map entry
	// while a second goroutine has already loaded the same *sync.Mutex
	// via lockFor's LoadOrStore, but not yet locked it, lets a third
	// goroutine's later lockFor call install a *different* mutex for the
	// same task ID — which would let the first and third goroutines run
	// their critical sections concurrently, silently reopening the exact
	// race this field exists to close. A correct version needs
	// reference-counted entries (a wrapper struct with a refcount
	// protected by its own small bookkeeping lock, decremented and
	// conditionally deleted by whichever critical section releases it
	// last) — real, but nontrivial, engineering that risks a subtle
	// concurrency bug of its own. Per this project's "correctness over
	// performance" rule, leaving the (bounded-severity, slow) leak in
	// place is the safer choice until that refcounting is written and
	// proven under -race; tracked as a follow-up, not silently ignored.
	//
	// handleTasksCancel used to call lockFor(params.ID) before checking
	// whether the task exists, so a tasks/cancel call for a nonexistent
	// task ID minted a permanent entry here at the cost of a bare HTTP
	// round trip — no real task, no LLM turn, faster than the accepted
	// leak's normal rate. handleTasksCancel now does an unlocked
	// s.tasks.Get existence check first and returns not-found without
	// ever calling lockFor, so only IDs that name a real (or once-real)
	// task mint an entry — back to the bounded-severity leak described
	// above, not the unthrottled version.
	taskLocks sync.Map // map[string]*sync.Mutex
}

// lockFor returns the *sync.Mutex serializing taskID's terminal-state
// decisions between handleTasksCancel and finishTurn, creating one on
// first use. Callers must hold this lock across the full
// read-current-state -> decide -> write sequence, not just the write, or
// the race it exists to close reopens.
func (s *Server) lockFor(taskID string) *sync.Mutex {
	actual, _ := s.taskLocks.LoadOrStore(taskID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// isTerminal reports whether state is one a Task does not transition out
// of. TaskRejected is included for forward-compatibility even though no
// handler in this package writes it today.
func isTerminal(state TaskState) bool {
	switch state {
	case TaskCompleted, TaskFailed, TaskCanceled, TaskRejected:
		return true
	default:
		return false
	}
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

	// Existence check BEFORE ever calling lockFor: lockFor's LoadOrStore
	// permanently mints a *sync.Mutex for params.ID (taskLocks is never
	// cleaned up — see the field comment), so calling it for an ID that
	// doesn't exist would let any caller grow that map at the cost of a
	// bare HTTP round trip, no LLM turn required. This unlocked read is
	// purely an existence gate, not a substitute for the lock-protected
	// read below: the actual cancel/no-op decision still comes entirely
	// from the re-read taken under the lock. TaskStore has no Delete
	// method, so "found" here can never become "not found" by the time
	// that locked re-read happens — no new TOCTOU gap is introduced by
	// this pre-check.
	if _, err := s.tasks.Get(ctx, params.ID); err != nil {
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}

	// Hold this task's lock across "read current state -> decide -> write"
	// so this can't race with finishTurn's own read-decide-write sequence
	// for the same task ID (see lockFor and finishTurn). Whichever side
	// acquires the lock first fully completes its decision — including the
	// write — before the other observes the task's state.
	mu := s.lockFor(params.ID)
	mu.Lock()
	task, err := s.tasks.Get(ctx, params.ID)
	if err != nil {
		mu.Unlock()
		s.writeError(w, req.ID, -32000, "task not found: "+err.Error())
		return
	}
	if isTerminal(task.State) {
		// The task already reached a terminal state — its own turn
		// completed/failed, a prior cancel already landed, or it was
		// rejected — so canceling it now must be a no-op rather than an
		// overwrite. Returning the stored task as-is preserves whatever
		// terminal state it actually holds instead of forcing
		// TaskCanceled over it.
		mu.Unlock()
		s.cancelRunningTurn(task.ID)
		result, _ := json.Marshal(task)
		s.writeResult(w, req.ID, result)
		return
	}
	task.State = TaskCanceled
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}
	mu.Unlock()

	// cancelRunningTurn is called outside the lock to keep mu's critical
	// section limited to the persistence decision above — not because
	// holding it here would deadlock. (context.CancelFunc never blocks, so
	// calling it while another goroutine is parked on mu would be harmless
	// too; this is a scope-minimization choice, not a safety requirement.)
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

// finishTurn decides what to persist after runTurn completes for task, and
// persists it itself. If the store already holds a terminal state for this
// task's ID — in practice, TaskCanceled written by a concurrent
// tasks/cancel — the stored task is returned as-is and task's
// locally-computed terminal state (TaskCompleted/TaskFailed) is discarded
// rather than persisted.
//
// Concurrency: the read-decide-write sequence runs under
// s.lockFor(task.ID), the same per-task lock handleTasksCancel takes
// around its own read-decide-write sequence. That closes the race this
// method's doc comment used to describe as a "known limitation" requiring
// a TaskStore compare-and-swap primitive — no such primitive is needed,
// because both writers of a task's terminal state are in-process on the
// same *Server and can serialize through an ordinary mutex instead of
// needing atomicity at the storage layer.
//
// What's still open: this guarantee holds only for writers that take
// s.lockFor(task.ID) around their read-decide-write sequence. Today that's
// exactly this method and handleTasksCancel — the only two places that
// write a task's terminal state. A hypothetical future writer added
// without going through lockFor would reopen the race; nothing enforces
// that discipline except this comment.
func (s *Server) finishTurn(ctx context.Context, task Task) (result Task, alreadyTerminal bool) {
	mu := s.lockFor(task.ID)
	mu.Lock()
	defer mu.Unlock()

	if current, err := s.tasks.Get(ctx, task.ID); err == nil && isTerminal(current.State) {
		return current, true
	}
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
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
	defer done()
	task = s.runTurn(turnCtx, task, messageText(params.Message))

	task, _ = s.finishTurn(ctx, task)

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
	defer done()
	task = s.runTurn(turnCtx, task, messageText(params.Message))

	task, _ = s.finishTurn(ctx, task)
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
