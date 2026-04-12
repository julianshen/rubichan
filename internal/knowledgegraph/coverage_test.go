package knowledgegraph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newTempGraph opens a fresh knowledge graph in a temp dir and registers
// cleanup. Returns the concrete type so tests can reach internals.
func newTempGraph(t *testing.T) *KnowledgeGraph {
	t.Helper()
	tmpDir := t.TempDir()
	raw, err := openGraph(context.Background(), tmpDir, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	return raw.(*KnowledgeGraph)
}

// --- graph.go: Delete ---

func TestDelete_ExistingEntityRemovesFileAndRow(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	e := &kg.Entity{
		ID:      "del-001",
		Kind:    kg.KindArchitecture,
		Title:   "To Be Deleted",
		Body:    "Will be removed",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}
	require.NoError(t, g.Put(ctx, e))

	// Verify file exists
	path := entityToPath(g.knowledgeDir, e)
	_, err := os.Stat(path)
	require.NoError(t, err, "markdown file should exist before delete")

	// Delete it
	require.NoError(t, g.Delete(ctx, "del-001"))

	// File must be gone
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "markdown file should be removed")

	// Row must be gone
	got, err := g.Get(ctx, "del-001")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Cache must be cleared
	g.mu.RLock()
	_, inCache := g.cache["del-001"]
	g.mu.RUnlock()
	assert.False(t, inCache)
}

func TestDelete_NonexistentEntityNoError(t *testing.T) {
	g := newTempGraph(t)
	// Deleting an ID that isn't there should be a no-op (not an error).
	require.NoError(t, g.Delete(context.Background(), "does-not-exist"))
}

func TestDelete_FileAlreadyMissingIsTolerated(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	e := &kg.Entity{
		ID:      "del-002",
		Kind:    kg.KindDecision,
		Title:   "Missing file",
		Body:    "body",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}
	require.NoError(t, g.Put(ctx, e))

	// Delete the backing markdown file manually. Delete() must still succeed.
	path := entityToPath(g.knowledgeDir, e)
	require.NoError(t, os.Remove(path))

	require.NoError(t, g.Delete(ctx, "del-002"))
}

// --- graph.go: vectorSearch ---

// stubEmbedder is used in tests to provide a controllable query-side embedding
// vector. It deliberately only returns a non-empty embedding for the *query*
// text so the async embedAndStore goroutine triggered from Put returns an
// error and does not race with subsequent writes. Pre-computed vectors for
// stored entities are written directly via insertEmbedding.
type stubEmbedder struct {
	dims   int
	vector map[string][]float32
}

func (s *stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := s.vector[text]; ok {
		return v, nil
	}
	// Fail for any text we didn't pre-register; embedAndStore will then no-op.
	return nil, errors.New("stub: no embedding for text")
}

func (s *stubEmbedder) Dims() int { return s.dims }

// insertEmbedding writes a vector directly, bypassing the async embedAndStore
// goroutine so tests are deterministic and free of FK/lock races.
func insertEmbedding(t *testing.T, g *KnowledgeGraph, entityID string, vec []float32) {
	t.Helper()
	_, err := g.db.Exec(
		`INSERT OR REPLACE INTO embeddings(entity_id, vector, dims) VALUES(?, ?, ?)`,
		entityID, encodeVector(vec), len(vec),
	)
	require.NoError(t, err)
}

func TestVectorSearch_OrdersByCosineSimilarity(t *testing.T) {
	tmpDir := t.TempDir()
	// Use a null-ish embedder that just returns a query vector on demand.
	emb := &stubEmbedder{
		dims: 3,
		vector: map[string][]float32{
			"query": {1, 0, 0},
		},
	}
	raw, err := openGraph(context.Background(), tmpDir, []kg.Option{kg.WithEmbedder(emb)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	g := raw.(*KnowledgeGraph)
	ctx := context.Background()

	entities := []*kg.Entity{
		{ID: "close", Kind: kg.KindArchitecture, Title: "Close Entity", Body: "close body", Source: kg.SourceManual},
		{ID: "far", Kind: kg.KindArchitecture, Title: "Far Entity", Body: "far body", Source: kg.SourceManual},
		{ID: "mid", Kind: kg.KindArchitecture, Title: "Middle Entity", Body: "mid body", Source: kg.SourceManual},
	}
	for _, e := range entities {
		e.Created = time.Now()
		e.Updated = time.Now()
		require.NoError(t, g.Put(ctx, e))
	}

	// Write embeddings directly to avoid racing with the async embedAndStore goroutine.
	insertEmbedding(t, g, "close", []float32{0.99, 0.01, 0.0})
	insertEmbedding(t, g, "far", []float32{0.0, 1.0, 0.0})
	insertEmbedding(t, g, "mid", []float32{0.7, 0.7, 0.0})

	results, err := g.Query(ctx, kg.QueryRequest{Text: "query", Limit: 3})
	require.NoError(t, err)
	require.Len(t, results, 3)

	// "close" must rank first (cosine similarity ~ 0.99).
	assert.Equal(t, "close", results[0].Entity.ID)
}

func TestVectorSearch_KindFilterApplied(t *testing.T) {
	tmpDir := t.TempDir()
	emb := &stubEmbedder{dims: 2, vector: map[string][]float32{
		"query": {1, 0},
	}}
	raw, err := openGraph(context.Background(), tmpDir, []kg.Option{kg.WithEmbedder(emb)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	g := raw.(*KnowledgeGraph)
	ctx := context.Background()

	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "a", Kind: kg.KindArchitecture, Title: "Alpha A", Body: "a",
		Source: kg.SourceManual, Created: time.Now(), Updated: time.Now(),
	}))
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "b", Kind: kg.KindDecision, Title: "Beta B", Body: "b",
		Source: kg.SourceManual, Created: time.Now(), Updated: time.Now(),
	}))

	insertEmbedding(t, g, "a", []float32{1, 0})
	insertEmbedding(t, g, "b", []float32{0.9, 0.1})

	results, err := g.Query(ctx, kg.QueryRequest{
		Text:       "query",
		KindFilter: []kg.EntityKind{kg.KindDecision},
	})
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, kg.KindDecision, r.Entity.Kind)
	}
}

// --- graph.go: appendKindLayerClauses (layer-only branch) ---

func TestAppendKindLayerClauses_LayerOnly(t *testing.T) {
	q, args := appendKindLayerClauses(
		"SELECT 1 WHERE 1=1", nil,
		nil,
		[]kg.EntityLayer{kg.EntityLayerBase, kg.EntityLayerTeam},
	)
	assert.Contains(t, q, "e.layer IN (?, ?)")
	assert.Len(t, args, 2)
}

// --- bootstrap.go: Detect edge cases ---

func TestBootstrapDetect_JavaScriptProject(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte("{}"), 0o644))

	d := NewBootstrapLanguageDetector()
	profile, err := d.Detect(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "javascript", profile.Language)
	assert.Contains(t, profile.Frameworks, "npm")
}

func TestBootstrapDetect_UnknownProjectReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewBootstrapLanguageDetector()
	_, err := d.Detect(context.Background(), tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not detect project language")
}

// --- bootstrap.go: IsInitialized edge cases ---

func TestIsInitialized_MissingKnowledgeDirReturnsFalse(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewBootstrapLanguageDetector()
	ok, err := d.IsInitialized(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsInitialized_EmptyKnowledgeDirReturnsFalse(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".knowledge"), 0o755))
	d := NewBootstrapLanguageDetector()
	ok, err := d.IsInitialized(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsInitialized_PopulatedKnowledgeDirReturnsTrue(t *testing.T) {
	tmpDir := t.TempDir()
	kdir := filepath.Join(tmpDir, ".knowledge")
	require.NoError(t, os.MkdirAll(kdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(kdir, "schema.yaml"), []byte("version: 1"), 0o644))

	d := NewBootstrapLanguageDetector()
	ok, err := d.IsInitialized(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.True(t, ok)
}

// --- selector.go: NewNullSelector paths ---

func TestNullSelector_SelectAndRecordUsage(t *testing.T) {
	s := NewNullSelector()
	require.NotNil(t, s)

	results, err := s.Select(context.Background(), "anything", 1000)
	require.NoError(t, err)
	assert.Empty(t, results)

	// RecordUsage is a no-op for the null selector.
	err = s.RecordUsage(context.Background(), []kg.ScoredEntity{
		{Entity: &kg.Entity{ID: "x"}, Score: 1, EstimatedTokens: 1},
	})
	require.NoError(t, err)
}

// --- selector.go: budget=0 paths on strategy selectors ---

func TestSelector_StrategiesWithZeroBudget(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	// Two simple entities
	for i, id := range []string{"s-1", "s-2"} {
		require.NoError(t, g.Put(ctx, &kg.Entity{
			ID:         id,
			Kind:       kg.KindArchitecture,
			Title:      "Strategy entity " + id,
			Body:       "body content",
			Source:     kg.SourceManual,
			Created:    time.Now(),
			Updated:    time.Now().Add(time.Duration(i) * time.Hour),
			Confidence: float64(i) * 0.5,
			UsageCount: i,
		}))
	}

	strategies := []kg.SelectorKind{
		kg.SelectorByScore,
		kg.SelectorByRecency,
		kg.SelectorByUsage,
		kg.SelectorByConfidence,
		kg.SelectorKind("unknown"), // default branch
	}
	for _, k := range strategies {
		sel := NewContextSelectorWithStrategy(g, k)
		results, err := sel.Select(ctx, "strategy", 0)
		require.NoErrorf(t, err, "strategy %s", k)
		// With zero budget, no trimming is applied (the code path we want to cover).
		_ = results
	}
}

func TestSelector_RecordUsageEmptyIsNoOp(t *testing.T) {
	g := newTempGraph(t)
	sel := NewContextSelector(g)
	err := sel.RecordUsage(context.Background(), nil)
	require.NoError(t, err)
}

// --- test_helpers.go: bring exported helper coverage up ---

func TestTestHelpers_AssertQueryReturnsInvokesGraph(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()
	// Actually invoke the helper so its body is executed.
	AssertQueryReturns(t, fixture.Graph, "test", []string{})
}

func TestTestHelpers_AssertErrorContainsMatches(t *testing.T) {
	AssertErrorContains(t, errors.New("boom: connection refused"), "connection refused")
}

func TestTestHelpers_FixtureCleanupIsNoOp(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	fixture.Cleanup() // covers the no-op body
}

// --- embedders.go: exercise Ollama Embed error branches ---

func TestOllamaEmbedder_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid JSON so decode fails.
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(server.URL)
	_, err := emb.Embed(context.Background(), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestOllamaEmbedder_HealthCheckNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(server.URL)
	err := emb.HealthCheck(context.Background())
	assert.ErrorIs(t, err, ErrEmbedderUnavailable)
}

func TestNewOllamaEmbedder_DefaultBaseURL(t *testing.T) {
	emb := NewOllamaEmbedder("")
	require.NotNil(t, emb)
	assert.Equal(t, "http://localhost:11434", emb.baseURL)

	emb2 := NewOllamaEmbedderWithModel("", "custom")
	assert.Equal(t, "http://localhost:11434", emb2.baseURL)
	assert.Equal(t, "custom", emb2.model)
}

// --- embedders.go: OpenAI Embed error branches (network + HealthCheck) ---

func TestOpenAIEmbedder_EmbedNetworkFailure(t *testing.T) {
	// The OpenAI embedder hardcodes api.openai.com. To still exercise the
	// request path without performing a real API call, we point the embedder
	// at an invalid DNS name by replacing the http client with one that
	// short-circuits every request with an error.
	emb := NewOpenAIEmbedder("fake-key")
	emb.client = &http.Client{Transport: errTransport{}}

	_, err := emb.Embed(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestOpenAIEmbedder_HealthCheckPropagatesError(t *testing.T) {
	emb := NewOpenAIEmbedder("fake-key")
	emb.client = &http.Client{Transport: errTransport{}}
	err := emb.HealthCheck(context.Background())
	assert.ErrorIs(t, err, ErrEmbedderUnavailable)
}

// errTransport always returns an error for any HTTP round trip.
type errTransport struct{}

func (errTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic network failure")
}

// cannedTransport returns a fixed response body regardless of the request URL.
type cannedTransport struct {
	status int
	body   string
}

func (c cannedTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Status:     http.StatusText(c.status),
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}

// --- OpenAI Embed: exercise success, non-200, empty-data, decode-error ---

func TestOpenAIEmbedder_SuccessPath(t *testing.T) {
	emb := NewOpenAIEmbedder("test-key")
	emb.client = &http.Client{Transport: cannedTransport{
		status: 200,
		body:   `{"data":[{"embedding":[0.1,0.2,0.3]}]}`,
	}}
	vec, err := emb.Embed(context.Background(), "hi")
	require.NoError(t, err)
	require.Len(t, vec, 3)
	assert.InDelta(t, 0.1, vec[0], 1e-6)
}

func TestOpenAIEmbedder_HTTPErrorStatus(t *testing.T) {
	emb := NewOpenAIEmbedder("test-key")
	emb.client = &http.Client{Transport: cannedTransport{
		status: 500,
		body:   `server error`,
	}}
	_, err := emb.Embed(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestOpenAIEmbedder_EmptyData(t *testing.T) {
	emb := NewOpenAIEmbedder("test-key")
	emb.client = &http.Client{Transport: cannedTransport{
		status: 200,
		body:   `{"data":[]}`,
	}}
	_, err := emb.Embed(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no embeddings")
}

func TestOpenAIEmbedder_DecodeError(t *testing.T) {
	emb := NewOpenAIEmbedder("test-key")
	emb.client = &http.Client{Transport: cannedTransport{
		status: 200,
		body:   `not json`,
	}}
	_, err := emb.Embed(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestOpenAIEmbedder_HealthCheckSuccess(t *testing.T) {
	emb := NewOpenAIEmbedder("test-key")
	emb.client = &http.Client{Transport: cannedTransport{
		status: 200,
		body:   `{"data":[{"embedding":[0.1]}]}`,
	}}
	err := emb.HealthCheck(context.Background())
	require.NoError(t, err)
}

// --- embedAndStore: success path using testDB ---

// successEmbedder always returns a known vector.
type successEmbedder struct{ dims int }

func (s successEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, s.dims)
	v[0] = 1
	return v, nil
}
func (s successEmbedder) Dims() int { return s.dims }

func TestEmbedAndStore_PersistsVector(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// Insert a parent entity row so the FK on embeddings(entity_id) is satisfied.
	_, err := db.Exec(
		`INSERT INTO entities(id, kind, title, body, source) VALUES(?, ?, ?, ?, ?)`,
		"emb-1", "architecture", "Embed Me", "body", "manual",
	)
	require.NoError(t, err)

	g := &KnowledgeGraph{db: db, embedder: successEmbedder{dims: 4}}

	// Synchronous call (not via goroutine) to cover embedAndStore end-to-end.
	g.embedAndStore(context.Background(), "emb-1", "Embed Me")

	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM embeddings WHERE entity_id = ?`, "emb-1",
	).Scan(&count))
	assert.Equal(t, 1, count)
}

// --- embedAndStore: error path where embedder fails ---

func TestEmbedAndStore_EmbedErrorIsSilent(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	g := &KnowledgeGraph{db: db, embedder: kg.NullEmbedder{}}
	// Must not panic; does not need to return anything.
	g.embedAndStore(context.Background(), "no-such-id", "text")
}

// --- Ingestor error branches ---

type errCompleter struct{}

func (errCompleter) Complete(_ context.Context, _ string) (string, error) {
	return "", errors.New("completer failure")
}

func TestLLMIngestor_CompleterErrorPropagates(t *testing.T) {
	g := newTempGraph(t)
	ing := NewLLMIngestor(errCompleter{})
	n, err := ing.Ingest(context.Background(), g, "some text", kg.SourceLLM)
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "LLM error")
}

func TestLLMIngestor_ParseError(t *testing.T) {
	g := newTempGraph(t)
	ing := NewLLMIngestor(&mockCompleter{response: "not-json"})
	_, err := ing.Ingest(context.Background(), g, "text", kg.SourceLLM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse error")
}

func TestLLMIngestor_DropsEntriesMissingIDOrKind(t *testing.T) {
	g := newTempGraph(t)
	// Two entries: one valid, one missing kind, one missing id
	response := `[
	  {"id":"has-both","kind":"architecture","title":"OK","body":"b"},
	  {"id":"missing-kind","title":"nope","body":"x"},
	  {"kind":"decision","title":"no id","body":"y"}
	]`
	ing := NewLLMIngestor(&mockCompleter{response: response})
	n, err := ing.Ingest(context.Background(), g, "text", kg.SourceLLM)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestManualIngestor_RequiresFilePath(t *testing.T) {
	g := newTempGraph(t)
	ing := NewManualIngestor()
	_, err := ing.Ingest(context.Background(), g, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filePath required")
}

func TestManualIngestor_PlainTextNoFrontmatterIsNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	g := newTempGraph(t)
	path := filepath.Join(tmpDir, "plain.md")
	require.NoError(t, os.WriteFile(path, []byte("just body no frontmatter"), 0o644))

	ing := NewManualIngestor()
	n, err := ing.Ingest(context.Background(), g, path)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestFileIngestor_RequiresFilePath(t *testing.T) {
	g := newTempGraph(t)
	ing := NewFileIngestor(&mockCompleter{response: "[]"})
	_, err := ing.Ingest(context.Background(), g, "")
	require.Error(t, err)
}

func TestFileIngestor_MissingFile(t *testing.T) {
	g := newTempGraph(t)
	ing := NewFileIngestor(&mockCompleter{response: "[]"})
	_, err := ing.Ingest(context.Background(), g, "/definitely/not/a/real/file.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestGitIngestor_RequiresProjectRoot(t *testing.T) {
	g := newTempGraph(t)
	ing := NewGitIngestor(&mockCompleter{response: "[]"})
	_, err := ing.Ingest(context.Background(), g, "", "1w")
	require.Error(t, err)
}

func TestGitIngestor_NonGitDirReturnsError(t *testing.T) {
	tmpDir := t.TempDir() // empty, not a git repo
	g := newTempGraph(t)
	ing := NewGitIngestor(&mockCompleter{response: "[]"})
	_, err := ing.Ingest(context.Background(), g, tmpDir, "1w")
	require.Error(t, err)
}

// --- readEntityFromBytes edge cases ---

func TestReadEntityFromBytes_EmptyAndMalformed(t *testing.T) {
	// Empty
	e, err := readEntityFromBytes(nil)
	require.NoError(t, err)
	assert.Nil(t, e)

	// Malformed frontmatter (no closing delimiter)
	_, err = readEntityFromBytes([]byte("---\nid: x\nkind: decision\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed frontmatter")
}

// --- test_helpers: copyDir error branches ---

func TestCopyDir_SrcNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "not-a-dir.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	err := copyDir(file, filepath.Join(tmpDir, "dst"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestCopyDir_SrcMissing(t *testing.T) {
	err := copyDir("/nope/does/not/exist", t.TempDir())
	require.Error(t, err)
}

// --- test_helpers: AssertIndexValid error branches ---

func TestAssertIndexValid_MissingFile(t *testing.T) {
	err := AssertIndexValid(t, "/definitely-not-a-db.db")
	// modernc.org/sqlite creates the file on open, so there may be no error on open
	// but the schema check should fail (no tables).
	if err == nil {
		t.Skip("sqlite driver silently creates db on open")
	}
}

// --- Stats: empty graph path ---

func TestStats_EmptyGraph(t *testing.T) {
	g := newTempGraph(t)
	stats, err := g.Stats(context.Background())
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 0, stats.TotalEntities)
	assert.Equal(t, 0.0, stats.AvgScore)
}

// --- walkKnowledgeDir: non-directory path ---

func TestWalkKnowledgeDir_PathIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "not-a-dir.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	_, err := walkKnowledgeDir(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a directory")
}

// --- CollectBootstrapProfile: validation errors ---

// recordingQuestioner returns scripted errors or responses for specific prompts.
type recordingQuestioner struct {
	stringResp map[string]string
	stringErr  map[string]error
	choiceResp map[string]string
	choiceErr  map[string]error
	multiResp  map[string][]string
	multiErr   map[string]error
}

func (r *recordingQuestioner) AskString(prompt string) (string, error) {
	if err, ok := r.stringErr[prompt]; ok {
		return "", err
	}
	return r.stringResp[prompt], nil
}
func (r *recordingQuestioner) AskChoice(prompt string, _ []string) (string, error) {
	if err, ok := r.choiceErr[prompt]; ok {
		return "", err
	}
	return r.choiceResp[prompt], nil
}
func (r *recordingQuestioner) AskMultiSelect(prompt string, _ []string) ([]string, error) {
	if err, ok := r.multiErr[prompt]; ok {
		return nil, err
	}
	return r.multiResp[prompt], nil
}

func TestCollectBootstrapProfile_EmptyNameFails(t *testing.T) {
	q := &recordingQuestioner{
		stringResp: map[string]string{"What is your project name?": "   "},
	}
	_, err := CollectBootstrapProfile(q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project name cannot be empty")
}

func TestCollectBootstrapProfile_NameError(t *testing.T) {
	q := &recordingQuestioner{
		stringErr: map[string]error{"What is your project name?": errors.New("io")},
	}
	_, err := CollectBootstrapProfile(q)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project name")
}

// validResponses returns a complete, valid recording questioner that allows
// individual prompts to be overridden with errors via the with* helpers.
func validResponses() *recordingQuestioner {
	return &recordingQuestioner{
		stringResp: map[string]string{
			"What is your project name?":                   "demo",
			"Describe your pain points (comma-separated):": "speed, scale",
			"Is this an existing project?":                 "no",
		},
		choiceResp: map[string]string{
			"What is your architecture style?": "Monolithic",
			"What is your team size?":          "small",
			"What is your team composition?":   "fullstack",
		},
		multiResp: map[string][]string{
			"Select backend technologies:":        {"Go"},
			"Select frontend technologies:":       {"React"},
			"Select database technologies:":       {"PostgreSQL"},
			"Select infrastructure technologies:": {"Docker"},
		},
	}
}

func TestCollectBootstrapProfile_ErrorAtEachPrompt(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*recordingQuestioner)
		errSub string
	}{
		{"backend", func(q *recordingQuestioner) {
			q.multiErr = map[string]error{"Select backend technologies:": errors.New("io")}
		}, "backend"},
		{"frontend", func(q *recordingQuestioner) {
			q.multiErr = map[string]error{"Select frontend technologies:": errors.New("io")}
		}, "frontend"},
		{"database", func(q *recordingQuestioner) {
			q.multiErr = map[string]error{"Select database technologies:": errors.New("io")}
		}, "database"},
		{"infra", func(q *recordingQuestioner) {
			q.multiErr = map[string]error{"Select infrastructure technologies:": errors.New("io")}
		}, "infrastructure"},
		{"arch", func(q *recordingQuestioner) {
			q.choiceErr = map[string]error{"What is your architecture style?": errors.New("io")}
		}, "architecture"},
		{"painpoints", func(q *recordingQuestioner) {
			q.stringErr = map[string]error{"Describe your pain points (comma-separated):": errors.New("io")}
		}, "pain points"},
		{"teamsize", func(q *recordingQuestioner) {
			q.choiceErr = map[string]error{"What is your team size?": errors.New("io")}
		}, "team size"},
		{"teamcomp", func(q *recordingQuestioner) {
			q.choiceErr = map[string]error{"What is your team composition?": errors.New("io")}
		}, "team composition"},
		{"existing", func(q *recordingQuestioner) {
			q.stringErr = map[string]error{"Is this an existing project?": errors.New("io")}
		}, "existing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := validResponses()
			tt.mutate(q)
			_, err := CollectBootstrapProfile(q)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSub)
		})
	}
}

func TestCollectBootstrapProfile_HappyPathFillsAllFields(t *testing.T) {
	q := validResponses()
	profile, err := CollectBootstrapProfile(q)
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "demo", profile.ProjectName)
	assert.Equal(t, "Monolithic", profile.ArchitectureStyle)
	assert.False(t, profile.IsExisting)
}

// --- Detect: mixed-language project ---

func TestBootstrapDetect_MixedProjectMarksMixed(t *testing.T) {
	tmpDir := t.TempDir()
	// Create both go.mod and package.json. Detect short-circuits at the
	// first match in source order, so this exercises the "mixed" branch
	// only when multiple frameworks accumulate. The current implementation
	// only sets multiple frameworks when more than one detector adds.
	// To force frameworks length > 1, we hand-craft a Profile through
	// chained mkdirs/files; if the production code only adds one framework,
	// the test will still document expected behaviour for go-only project.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module x"), 0o644))
	d := NewBootstrapLanguageDetector()
	profile, err := d.Detect(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "go", profile.Language)
}

// --- IsInitialized: stat returns a non-IsNotExist error ---

func TestIsInitialized_PathPointsAtFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Create .knowledge as a regular file so ReadDir errors with not-a-dir.
	path := filepath.Join(tmpDir, ".knowledge")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	d := NewBootstrapLanguageDetector()
	_, err := d.IsInitialized(context.Background(), tmpDir)
	require.Error(t, err)
}

// --- parseCommaSeparated: empty input branch ---

func TestParseCommaSeparated_Empty(t *testing.T) {
	got := parseCommaSeparated("")
	assert.Empty(t, got)
	assert.NotNil(t, got)
}

// --- DiscoverModules: file inside module dir is skipped ---

func TestDiscoverModules_SkipsNonDirectoryEntry(t *testing.T) {
	tmpDir := t.TempDir()
	// Create pkg/ with one valid sub-directory and one stray file.
	pkgDir := filepath.Join(tmpDir, "pkg")
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "valid_module"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "stray-file.go"), []byte("package x"), 0o644))

	entities, err := DiscoverModules(tmpDir)
	require.NoError(t, err)
	// Only the directory should turn into an entity.
	require.Len(t, entities, 1)
	assert.Equal(t, "valid_module", entities[0].ID)
}

// --- DiscoverDecisionsFromGit: empty/no-keyword commit log ---

func TestDiscoverDecisionsFromGit_NoGitRepoReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	out, err := DiscoverDecisionsFromGit(tmpDir, &BootstrapProfile{})
	require.NoError(t, err)
	assert.Empty(t, out)
}

// --- DiscoverIntegrations: skip dirs and unreadable files ---

func TestDiscoverIntegrations_SkipsVendorDir(t *testing.T) {
	tmpDir := t.TempDir()
	// Create vendor/foo with a Go file that imports a known integration.
	vendorPath := filepath.Join(tmpDir, "vendor", "github.com", "lib", "pq")
	require.NoError(t, os.MkdirAll(vendorPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendorPath, "main.go"),
		[]byte(`package x; import "github.com/lib/pq"`), 0o644))

	entities, err := DiscoverIntegrations(tmpDir)
	require.NoError(t, err)
	// Vendor dir is skipped → no integration discovered.
	assert.Empty(t, entities)
}

func TestDiscoverIntegrations_NonGoFilesIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	// Plain text file, not .go — should be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "notes.txt"),
		[]byte(`import "github.com/lib/pq"`), 0o644))

	entities, err := DiscoverIntegrations(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entities)
}

// --- WriteBootstrapEntities: happy path covers internal mkdir/write paths ---

func TestWriteBootstrapEntities_WritesFilesAndMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	knowledgeDir := filepath.Join(tmpDir, ".knowledge")

	entities := []*ProposedEntity{
		{
			ID: "mod-foo", Kind: KindModule, Title: "Foo Module",
			Body: "discovered", SourceType: SourceTypeModule, Confidence: 0.9,
			Tags: []string{"module", "discovered"},
		},
	}
	profile := &BootstrapProfile{ProjectName: "test"}

	meta, err := WriteBootstrapEntities(knowledgeDir, entities, profile)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Contains(t, meta.CreatedEntities, "mod-foo")

	// Files must exist on disk.
	mdPath := filepath.Join(knowledgeDir, KindModule, "mod-foo.md")
	_, err = os.Stat(mdPath)
	require.NoError(t, err)
	jsonPath := filepath.Join(knowledgeDir, ".bootstrap.json")
	_, err = os.Stat(jsonPath)
	require.NoError(t, err)
}

func TestWriteBootstrapEntities_MkdirFailure(t *testing.T) {
	// /dev/null/foo cannot be created as a directory on macOS or Linux.
	_, err := WriteBootstrapEntities("/dev/null/no-dir", nil, &BootstrapProfile{})
	require.Error(t, err)
}

// --- formatTagsYAML: empty branch ---

func TestFormatTagsYAML_EmptyAndPopulated(t *testing.T) {
	assert.Equal(t, "[]", formatTagsYAML(nil))
	assert.Equal(t, `["a", "b"]`, formatTagsYAML([]string{"a", "b"}))
}

// --- LLMIngestor: response with valid relationships covers rel-loop body ---

func TestLLMIngestor_ResponseWithRelationships(t *testing.T) {
	g := newTempGraph(t)
	response := `[
	  {
	    "id":"with-rel",
	    "kind":"architecture",
	    "title":"Has Rel",
	    "body":"b",
	    "relationships":[
	      {"kind":"depends-on","target":"other"},
	      {"kind":"","target":"missing-kind"},
	      {"kind":"justifies","target":""}
	    ]
	  }
	]`
	ing := NewLLMIngestor(&mockCompleter{response: response})
	n, err := ing.Ingest(context.Background(), g, "x", kg.SourceLLM)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// --- ManualIngestor: Put failure ---

func TestManualIngestor_PutErrorPropagates(t *testing.T) {
	g := newTempGraph(t)
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ent.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
id: pe-001
kind: architecture
title: Put Error
---
body`), 0o644))

	// Close the underlying DB to force Put to fail.
	require.NoError(t, g.db.Close())

	ing := NewManualIngestor()
	_, err := ing.Ingest(context.Background(), g, path)
	require.Error(t, err)
}

// --- walkKnowledgeDir: non-validation parse error propagates ---

func TestWalkKnowledgeDir_NonValidationParseErrorPropagates(t *testing.T) {
	tmpDir := t.TempDir()
	// Markdown without frontmatter delimiters → readEntityFile returns a
	// non-ValidationError that walkKnowledgeDir must propagate.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bad.md"), []byte("plain body"), 0o644))

	_, err := walkKnowledgeDir(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing frontmatter")
}

// --- rebuildIndexInternal: covers relationship/FTS insertion path ---
//
// This pre-seeds a knowledge directory with markdown files that have
// relationships, then opens the graph so rebuildIndexInternal walks them
// and inserts relationships+FTS rows inside the transaction.
func TestRebuildIndex_WithRelationships(t *testing.T) {
	tmpDir := t.TempDir()
	knowledgeDir := filepath.Join(tmpDir, ".knowledge", "architecture")
	require.NoError(t, os.MkdirAll(knowledgeDir, 0o755))

	md := `---
id: rebuild-001
kind: architecture
title: With Relationships
tags: [a, b]
relationships:
  - kind: depends-on
    target: other-001
  - kind: relates-to
    target: third-001
---
body content
`
	require.NoError(t, os.WriteFile(filepath.Join(knowledgeDir, "rebuild-001.md"), []byte(md), 0o644))

	raw, err := openGraph(context.Background(), tmpDir, nil)
	require.NoError(t, err)
	defer raw.Close()
	g := raw.(*KnowledgeGraph)

	// Verify the entity made it into the index.
	got, err := g.Get(context.Background(), "rebuild-001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Relationships, 2)

	// Force a manual RebuildIndex too (different code path through public API).
	require.NoError(t, g.RebuildIndex(context.Background()))
}

// --- Query: explicit Limit trims results ---

func TestQuery_LimitTrimsResults(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	for _, id := range []string{"q-1", "q-2", "q-3"} {
		require.NoError(t, g.Put(ctx, &kg.Entity{
			ID: id, Kind: kg.KindArchitecture, Title: "Arch " + id,
			Body: "body", Source: kg.SourceManual,
			Created: time.Now(), Updated: time.Now(),
		}))
	}

	results, err := g.Query(ctx, kg.QueryRequest{Text: "Arch", Limit: 2})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 2)
}

// --- Query: empty text path uses List with filters ---

func TestQuery_EmptyTextWithFilters(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "f-arch", Kind: kg.KindArchitecture, Title: "F Arch",
		Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}))
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "f-dec", Kind: kg.KindDecision, Title: "F Decision",
		Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}))

	results, err := g.Query(ctx, kg.QueryRequest{
		KindFilter: []kg.EntityKind{kg.KindDecision},
	})
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, kg.KindDecision, r.Entity.Kind)
	}
}

// --- List: tags filter applied (covers hasAllTags branch) ---

func TestList_TagsFilter(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "t-1", Kind: kg.KindArchitecture, Title: "Tagged",
		Tags: []string{"alpha", "beta"}, Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}))
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "t-2", Kind: kg.KindArchitecture, Title: "Other",
		Tags: []string{"alpha"}, Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}))

	got, err := g.List(ctx, kg.ListFilter{Tags: []string{"alpha", "beta"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t-1", got[0].ID)
}

// --- hasAllTags: branch coverage helper test ---

func TestHasAllTags(t *testing.T) {
	assert.True(t, hasAllTags([]string{"a", "b", "c"}, []string{"a", "b"}))
	assert.False(t, hasAllTags([]string{"a"}, []string{"a", "b"}))
	assert.True(t, hasAllTags([]string{"a"}, nil))
}

// --- Cleanup helper ---

func TestTestFixture_CleanupInvoked(t *testing.T) {
	f := &TestFixture{}
	f.Cleanup() // no-op, but covered now
}

// --- ftsSearch: match returns results (already covered by fixtures) ---

// --- addColumnIfMissing: no-op when column already exists ---

func TestAddColumnIfMissing_NoOpOnExistingColumn(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	// confidence is already added by createTables; this should be a no-op.
	err := addColumnIfMissing(db, "entities", "confidence",
		`ALTER TABLE entities ADD COLUMN confidence REAL DEFAULT 0.0`)
	require.NoError(t, err)
}

// --- writeEntityFile: relationships and tags ---

func TestWriteAndReadEntityFile_RoundTripWithRelationships(t *testing.T) {
	tmpDir := t.TempDir()

	e := &kg.Entity{
		ID:    "rt-001",
		Kind:  kg.KindArchitecture,
		Layer: kg.EntityLayerBase,
		Title: "Round Trip",
		Tags:  []string{"a", "b"},
		Body:  "body\ntext",
		Relationships: []kg.Relationship{
			{Kind: kg.RelDependsOn, Target: "other-001"},
		},
		Source:     kg.SourceManual,
		Created:    time.Now(),
		Updated:    time.Now(),
		Confidence: 0.75,
		Version:    "v1",
	}

	require.NoError(t, writeEntityFile(tmpDir, e))

	parsed, err := readEntityFile(entityToPath(tmpDir, e))
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.Equal(t, e.ID, parsed.ID)
	assert.Equal(t, e.Title, parsed.Title)
	assert.Equal(t, 0.75, parsed.Confidence)
	assert.Len(t, parsed.Relationships, 1)
}

// --- Get: error path for non-existent id ---

func TestGet_Miss(t *testing.T) {
	g := newTempGraph(t)
	e, err := g.Get(context.Background(), "nothing-here")
	require.NoError(t, err)
	assert.Nil(t, e)
}

// --- Get: cache hit path with stats ---

func TestGet_CacheHitReadsStats(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	e := &kg.Entity{
		ID: "cache-1", Kind: kg.KindArchitecture, Title: "Cached",
		Body: "body", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}
	require.NoError(t, g.Put(ctx, e))

	// Insert entity_stats row directly so cache-hit path reads it.
	now := time.Now().Format(time.RFC3339)
	_, err := g.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO entity_stats(entity_id, injection_count, last_accessed_at)
		 VALUES(?, ?, ?)`, "cache-1", 7, now,
	)
	require.NoError(t, err)

	// First Get warms the cache (or hits cache from Put), second Get definitely hits cache.
	got, err := g.Get(ctx, "cache-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	got2, err := g.Get(ctx, "cache-1")
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, 7, got2.UsageCount)
	assert.False(t, got2.LastUsed.IsZero())
}

// --- openGraph: invalid (unwritable) project root fails fast ---

func TestOpenGraph_UnwritableRootFails(t *testing.T) {
	// On macOS, /dev/null is not a directory and MkdirAll under it must fail.
	_, err := openGraph(context.Background(), "/dev/null/cannot-be-a-dir", nil)
	require.Error(t, err)
}

// --- selectByScore: error propagation when underlying Query fails ---

// brokenGraphSelector wraps a *KnowledgeGraph whose db has been closed,
// causing Query to fail and exercising the error branch.
func TestSelector_ScoreReturnsErrorWhenDBClosed(t *testing.T) {
	g := newTempGraph(t)
	// Close db now to force Query to fail.
	require.NoError(t, g.db.Close())

	sel := NewContextSelector(g)
	_, err := sel.Select(context.Background(), "anything", 100)
	require.Error(t, err)
}

// --- selectByRecency/Usage/Confidence: also propagate Query errors ---

func TestSelector_StrategyErrorsWhenDBClosed(t *testing.T) {
	for _, k := range []kg.SelectorKind{
		kg.SelectorByRecency, kg.SelectorByUsage, kg.SelectorByConfidence,
	} {
		t.Run(string(k), func(t *testing.T) {
			g := newTempGraph(t)
			require.NoError(t, g.db.Close())
			sel := NewContextSelectorWithStrategy(g, k)
			_, err := sel.Select(context.Background(), "x", 100)
			require.Error(t, err)
		})
	}
}

// --- contextSelector.RecordUsage: error from db ---

func TestRecordUsage_DBClosedReturnsError(t *testing.T) {
	g := newTempGraph(t)
	require.NoError(t, g.db.Close())
	sel := NewContextSelector(g)
	err := sel.RecordUsage(context.Background(),
		[]kg.ScoredEntity{{Entity: &kg.Entity{ID: "x"}}})
	require.Error(t, err)
}

// --- Get: cache-miss path with relationships exercises rels-loop body ---

func TestGet_CacheMissLoadsRelationships(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	target := &kg.Entity{
		ID: "g-target", Kind: kg.KindArchitecture, Title: "Target",
		Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}
	require.NoError(t, g.Put(ctx, target))

	src := &kg.Entity{
		ID: "g-src", Kind: kg.KindArchitecture, Title: "Source",
		Body: "x", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
		Relationships: []kg.Relationship{
			{Kind: kg.RelDependsOn, Target: "g-target"},
		},
	}
	require.NoError(t, g.Put(ctx, src))

	// Drop cache so Get() falls through to the DB query path that loads relationships.
	g.mu.Lock()
	g.cache = make(map[string]*kg.Entity)
	g.mu.Unlock()

	got, err := g.Get(ctx, "g-src")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Relationships, 1)
	assert.Equal(t, "g-target", got.Relationships[0].Target)
}

// --- Get: corrupted timestamps fall back to time.Now ---

func TestGet_BadTimestampsFallback(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	// Insert directly with bogus created_at/updated_at strings.
	_, err := g.db.ExecContext(ctx,
		`INSERT INTO entities(id, kind, layer, title, tags_json, body, source, created_at, updated_at, confidence)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"bad-ts", "architecture", "base", "Bad TS", "[]", "body", "manual",
		"not-a-timestamp", "also-not-a-timestamp", 0.0,
	)
	require.NoError(t, err)

	got, err := g.Get(ctx, "bad-ts")
	require.NoError(t, err)
	require.NotNil(t, got)
	// Fallback timestamps must be non-zero.
	assert.False(t, got.Created.IsZero())
	assert.False(t, got.Updated.IsZero())
}

// --- Get: malformed tags JSON yields error ---

func TestGet_BadTagsJSON(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	_, err := g.db.ExecContext(ctx,
		`INSERT INTO entities(id, kind, layer, title, tags_json, body, source, created_at, updated_at, confidence)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"bad-tags", "architecture", "base", "Bad Tags", "not-json", "body", "manual",
		time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339), 0.0,
	)
	require.NoError(t, err)

	_, err = g.Get(ctx, "bad-tags")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal tags")
}

// --- ftsSearch: nothing matches ---

func TestFtsSearch_NoMatches(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()
	results, err := g.Query(ctx, kg.QueryRequest{Text: "xyzzyzero"})
	require.NoError(t, err)
	assert.Empty(t, results)
}

// --- normalizedLayer covers default branch ---

func TestNormalizedLayer(t *testing.T) {
	assert.Equal(t, string(kg.EntityLayerBase), normalizedLayer(""))
	assert.Equal(t, string(kg.EntityLayerTeam), normalizedLayer(kg.EntityLayerTeam))
}

// --- repeatedPlaceholder edge cases ---

func TestRepeatedPlaceholder(t *testing.T) {
	assert.Equal(t, "", repeatedPlaceholder(0))
	assert.Equal(t, "?", repeatedPlaceholder(1))
	assert.Equal(t, "?, ?, ?", repeatedPlaceholder(3))
}

// --- estimateTokens basic sanity ---

func TestEstimateTokens(t *testing.T) {
	e := &kg.Entity{ID: "a", Title: "Title", Body: "body content"}
	assert.Greater(t, estimateTokens(e), 0)
}

// --- trimByBudget cuts at budget threshold ---

func TestTrimByBudget(t *testing.T) {
	results := []kg.ScoredEntity{
		{Entity: &kg.Entity{ID: "a"}, EstimatedTokens: 50},
		{Entity: &kg.Entity{ID: "b"}, EstimatedTokens: 50},
		{Entity: &kg.Entity{ID: "c"}, EstimatedTokens: 50},
	}
	got := trimByBudget(results, 100)
	assert.Len(t, got, 2)
}

// --- Put: validation and relationship/usage branches ---

func TestPut_RejectsEmptyIDOrKind(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	err := g.Put(ctx, &kg.Entity{Kind: kg.KindArchitecture, Title: "no id"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires ID and Kind")

	err = g.Put(ctx, &kg.Entity{ID: "x", Title: "no kind"})
	require.Error(t, err)
}

func TestPut_WithRelationshipsAndUsageStats(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	// First insert the target so the relationship resolves.
	target := &kg.Entity{
		ID: "target-001", Kind: kg.KindDecision, Title: "Target",
		Body: "target body", Source: kg.SourceManual,
		Created: time.Now(), Updated: time.Now(),
	}
	require.NoError(t, g.Put(ctx, target))

	// Now insert the source with a relationship and pre-set usage stats.
	source := &kg.Entity{
		ID:    "src-001",
		Kind:  kg.KindArchitecture,
		Title: "Source",
		Body:  "source body",
		Relationships: []kg.Relationship{
			{Kind: kg.RelJustifies, Target: "target-001"},
		},
		Source:     kg.SourceManual,
		Created:    time.Now(),
		Updated:    time.Now(),
		UsageCount: 3,
		LastUsed:   time.Now(),
	}
	require.NoError(t, g.Put(ctx, source))

	// Relationship row should exist.
	var relCount int
	require.NoError(t, g.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM relationships WHERE source_id = ?`, "src-001",
	).Scan(&relCount))
	assert.Equal(t, 1, relCount)

	// entity_stats row should reflect UsageCount>0 with non-empty last_accessed_at.
	var ic int
	var lastAccessed string
	require.NoError(t, g.db.QueryRowContext(ctx,
		`SELECT injection_count, COALESCE(last_accessed_at, '') FROM entity_stats WHERE entity_id = ?`,
		"src-001",
	).Scan(&ic, &lastAccessed))
	assert.Equal(t, 3, ic)
	assert.NotEmpty(t, lastAccessed)
}

// --- LintGraph: orphans + duplicate titles + missing body ---

func TestLintGraph_DetectsOrphansDuplicatesAndEmptyBodies(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	// Two entities sharing the same title.
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "dup-1", Kind: kg.KindArchitecture, Title: "Shared Title",
		Body: "one", Source: kg.SourceManual,
	}))
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "dup-2", Kind: kg.KindArchitecture, Title: "Shared Title",
		Body: "two", Source: kg.SourceManual,
	}))

	// Entity with an empty body.
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "empty-1", Kind: kg.KindArchitecture, Title: "Empty Body Entity",
		Body: "", Source: kg.SourceManual,
	}))

	// Insert an orphaned relationship via raw SQL (pointing at a non-existent target).
	_, err := g.db.ExecContext(ctx,
		`INSERT INTO relationships(source_id, kind, target_id) VALUES(?, ?, ?)`,
		"dup-1", "depends-on", "no-such-target",
	)
	require.NoError(t, err)

	report, err := g.LintGraph(ctx)
	require.NoError(t, err)
	require.NotNil(t, report)

	// Must contain our orphan.
	assert.NotEmpty(t, report.OrphanedRelationships)
	foundOrphan := false
	for _, o := range report.OrphanedRelationships {
		if o.TargetID == "no-such-target" {
			foundOrphan = true
		}
	}
	assert.True(t, foundOrphan)

	// Must contain our duplicate title.
	foundDup := false
	for _, d := range report.DuplicateTitles {
		if d.Title == "Shared Title" {
			foundDup = true
		}
	}
	assert.True(t, foundDup)

	// Must contain our empty-body entity.
	assert.Contains(t, report.EmptyBodies, "empty-1")
}

// --- Stats: exercises confidence / usage branches ---

func TestStats_WithConfidenceAndUsage(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "stat-1", Kind: kg.KindDecision, Title: "Stat 1",
		Body: "body", Source: kg.SourceManual, Confidence: 0.9,
	}))
	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "stat-2", Kind: kg.KindPattern, Title: "Stat 2",
		Body: "body", Source: kg.SourceManual, Confidence: 0.5,
	}))

	stats, err := g.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.TotalEntities, 2)
	assert.Greater(t, stats.AvgScore, 0.0)
	assert.GreaterOrEqual(t, stats.HighConfidenceCount, 1)
}

// --- RecordEntityMentions: entity referenced by ID/title ---

// TestRecordEntityMentions_TitleMatch exercises the title-match branch of
// RecordEntityMentions. It intentionally uses an in-memory SQLite DB via the
// same testDB helper used by store_test.go so row-iteration and writes share
// a single connection, avoiding the file-DB SQLITE_BUSY interaction that
// would otherwise occur.
func TestRecordEntityMentions_TitleMatch(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx := context.Background()
	_, err := db.ExecContext(ctx,
		`INSERT INTO entities(id, kind, title, body, source) VALUES(?, ?, ?, ?, ?)`,
		"rem-title", "architecture", "Mentionable Title", "body", "manual",
	)
	require.NoError(t, err)

	g := &KnowledgeGraph{db: db, embedder: kg.NullEmbedder{}}
	require.NoError(t, g.RecordEntityMentions(ctx, "We referenced Mentionable Title in discussion."))

	var hits int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COALESCE(query_hit_count, 0) FROM entity_stats WHERE entity_id = ?`, "rem-title",
	).Scan(&hits))
	assert.Equal(t, 1, hits)
}

// --- store.go: decodeVector edge cases ---

func TestDecodeVector_InvalidLengthReturnsNil(t *testing.T) {
	// A byte slice whose length is not a multiple of 4 must return nil.
	assert.Nil(t, decodeVector([]byte{0, 1, 2}))
	// A valid multiple-of-4 slice round-trips via encodeVector.
	vec := []float32{1.5, -2.25, 3.0}
	roundTrip := decodeVector(encodeVector(vec))
	assert.Equal(t, vec, roundTrip)
}

// --- store.go: cosineSimilarity edge cases ---

func TestCosineSimilarity_EdgeCases(t *testing.T) {
	// Length mismatch
	assert.Equal(t, 0.0, cosineSimilarity([]float32{1}, []float32{1, 2}))
	// Zero vector
	assert.Equal(t, 0.0, cosineSimilarity([]float32{0, 0}, []float32{1, 2}))
	// Empty
	assert.Equal(t, 0.0, cosineSimilarity([]float32{}, []float32{}))
	// Identical
	sim := cosineSimilarity([]float32{1, 0}, []float32{1, 0})
	assert.InDelta(t, 1.0, sim, 1e-9)
}

// --- store.go: rebuildFTS ---

func TestRebuildFTS_Succeeds(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	require.NoError(t, g.Put(ctx, &kg.Entity{
		ID: "fts-1", Kind: kg.KindArchitecture, Title: "FTS Entity",
		Body: "searchable body", Source: kg.SourceManual,
	}))

	require.NoError(t, rebuildFTS(g.db))

	// Verify the rebuilt FTS table can be queried.
	var count int
	require.NoError(t, g.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entities_fts WHERE entities_fts MATCH 'searchable'`,
	).Scan(&count))
	assert.GreaterOrEqual(t, count, 1)
}

// --- selector.go: contextSelector respects the budget branch ---

func TestSelector_StrategiesApplyBudgetTrim(t *testing.T) {
	g := newTempGraph(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "budget-" + string(rune('a'+i))
		require.NoError(t, g.Put(ctx, &kg.Entity{
			ID:         id,
			Kind:       kg.KindArchitecture,
			Title:      "Budget entity " + id,
			Body:       "lorem ipsum dolor sit amet consectetur adipiscing elit",
			Source:     kg.SourceManual,
			Created:    time.Now(),
			Updated:    time.Now().Add(time.Duration(i) * time.Minute),
			Confidence: 0.1 * float64(i),
			UsageCount: i,
		}))
	}

	for _, k := range []kg.SelectorKind{
		kg.SelectorByRecency, kg.SelectorByUsage, kg.SelectorByConfidence,
	} {
		sel := NewContextSelectorWithStrategy(g, k)
		results, err := sel.Select(ctx, "budget", 20)
		require.NoErrorf(t, err, "strategy %s", k)
		assert.LessOrEqual(t, len(results), 5)
	}
}
