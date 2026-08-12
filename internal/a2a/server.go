package a2a

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/julianshen/rubichan/internal/agent"
)

// Server answers A2A protocol requests by driving a real agent.Agent turn
// per task.
type Server struct {
	agent       *agent.Agent
	tasks       TaskStore
	card        AgentCard
	requireAuth bool
	apiKey      string
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
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
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
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}
	result, _ := json.Marshal(task)
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
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

	task = s.runTurn(ctx, task, messageText(params.Message))
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
	}

	result, _ := json.Marshal(task)
	_ = json.NewEncoder(w).Encode(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
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

	task = s.runTurn(ctx, task, messageText(params.Message))
	if err := s.tasks.Update(ctx, task); err != nil {
		log.Printf("warning: a2a server task store update failed for task %s: %v", task.ID, err)
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

// taskIDCounter is incremented atomically because HTTP handlers for
// concurrent message/send requests race on task ID generation otherwise.
var taskIDCounter atomic.Int64

func newTaskID() string {
	return "task-" + strconv.FormatInt(taskIDCounter.Add(1), 10)
}
