package agentsdk_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const modulePrefix = "github.com/julianshen/rubichan/"

// TestPublicPackagesHaveNoInternalImports enforces the modular-core
// redesign's central invariant (docs/MODULAR_CORE_REDESIGN.md §4.1): the
// public pkg/ SDK must not depend — even transitively — on any internal/
// package. That is what keeps the core portable: it could be promoted to
// its own Go module (Phase 1's end state, the step that lets a *different*
// module embed the agent) without dragging private code along.
//
// The compiler cannot catch a regression here: internal/ is importable
// from anywhere under the shared module root, so an accidental
// `internal/skills` or `internal/toolexec` import in pkg/ compiles cleanly
// and silently re-welds the core to a concrete subsystem. The design doc
// calls for gating this in CI rather than trusting the compiler — this
// test is that gate. Test files are intentionally exempt (Tests:false):
// an embedder consumes the non-test package.
func TestPublicPackagesHaveNoInternalImports(t *testing.T) {
	pinCacheToPkgTree(t)

	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps}
	roots, err := packages.Load(cfg, modulePrefix+"pkg/...")
	if err != nil {
		t.Fatalf("load pkg/...: %v", err)
	}
	if packages.PrintErrors(roots) > 0 {
		t.Fatal("errors loading pkg/... packages")
	}
	if len(roots) == 0 {
		t.Fatal("no pkg/ packages loaded — pattern or module resolution broke")
	}

	// BFS the transitive first-party import graph, remembering the chain
	// that reached each package so a violation reads as a blame trail.
	seen := map[string]bool{}
	type node struct {
		pkg *packages.Package
		via string
	}
	var queue []node
	for _, root := range roots {
		if !seen[root.PkgPath] {
			seen[root.PkgPath] = true
			queue = append(queue, node{root, root.PkgPath})
		}
	}

	var violations []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, imported := range sortedImports(current.pkg) {
			path := imported.PkgPath
			if strings.HasPrefix(path, modulePrefix+"internal/") {
				violations = append(violations, current.via+" -> "+path)
				continue
			}
			// Only first-party packages can reach our internal/ tree;
			// std and third-party deps cannot, so don't recurse into them.
			if !strings.HasPrefix(path, modulePrefix) || seen[path] {
				continue
			}
			seen[path] = true
			queue = append(queue, node{imported, current.via + " -> " + path})
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("public pkg/ packages must not import internal/ (transitively); "+
			"this breaks the redesign's portability invariant. Offending chains:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// pinCacheToPkgTree defeats Go's test-result cache for this gate.
//
// The scan runs `go list` in a subprocess (via packages.Load), whose file
// reads are invisible to the cache's file tracking. And because this test
// lives in pkg/agentsdk, its cached result is reused even when a *sibling*
// public package (pkg/skillsdk, …) — not a dependency of this test binary —
// gains a fresh internal/ import. A warm `go test ./...` would then print
// "ok (cached)" and never rescan, letting a violation slip through the very
// gate meant to catch it.
//
// Reading every .go file under pkg/ inside the test process records them in
// the cache's input set — and the directory reads WalkDir performs make an
// added or removed package change a scanned directory too — so any edit
// that could introduce a violation invalidates the cached pass. Over-reading
// only ever forces an extra (correct) rerun; it can never cause a false pass.
func pinCacheToPkgTree(t *testing.T) {
	t.Helper()
	// go test runs in the package directory (pkg/agentsdk); its parent is pkg/.
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		_, err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatalf("pin cache to pkg/ tree: %v", err)
	}
}

// sortedImports returns a package's direct imports ordered by path so BFS
// traversal and any failure output are deterministic (Imports is a map).
func sortedImports(p *packages.Package) []*packages.Package {
	out := make([]*packages.Package, 0, len(p.Imports))
	for _, imp := range p.Imports {
		out = append(out, imp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PkgPath < out[j].PkgPath })
	return out
}
