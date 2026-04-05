// Package knowledgegraph provides a local, shareable knowledge graph
// that accumulates project knowledge from day-to-day work.
//
// Storage is markdown-first: the .knowledge/ directory is the canonical
// source of truth (git-committable and human-editable). SQLite (.index.db)
// is a local, rebuild-able index that is never committed.
//
// Entities are stored as markdown files with YAML frontmatter in
// .knowledge/<kind>/<id>.md. The knowledge graph supports multiple
// update sources: LLM-generated, git history analysis, manual creation,
// and project file extraction (AGENT.md, CLAUDE.md).
//
// Query uses vector embeddings (Ollama, OpenAI, or SQLite FTS5 fallback)
// and token-budget-aware selection for injecting relevant knowledge into
// the agent's system prompt.
//
// Usage:
//
//	import "github.com/julianshen/rubichan/pkg/knowledgegraph"
//
//	g, err := knowledgegraph.Open(ctx, projectRoot)
//	if err != nil { ... }
//	defer g.Close()
//
//	// Write an entity
//	entity := &knowledgegraph.Entity{
//		ID:    "adr-001-go-language",
//		Kind:  knowledgegraph.KindArchitecture,
//		Title: "Go Language Choice",
//		Tags:  []string{"language", "runtime"},
//		Body:  "Go was chosen for single-binary distribution...",
//		Source: knowledgegraph.SourceManual,
//	}
//	err = g.Put(ctx, entity)
//
//	// Query for relevant knowledge
//	results, err := g.Query(ctx, knowledgegraph.QueryRequest{
//		Text: "sqlite concurrency patterns",
//		TokenBudget: 2000,
//	})
//	for _, r := range results {
//		fmt.Printf("Score: %.2f, Tokens: %d\n", r.Score, r.EstimatedTokens)
//		fmt.Println(r.Entity.Body)
//	}
package knowledgegraph
