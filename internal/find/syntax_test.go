package find

import (
	"context"
	"strings"
	"testing"
)

func TestFindAddsBoundedGoSyntaxEvidence(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"internal/worker/worker.go": `package worker

type Runner struct{}

func (r *Runner) Execute() {}

func Call(r *Runner) {
	r.Execute()
}

var ExecuteRef = (*Runner).Execute

// Execute appears in a comment only.
`,
		"internal/worker/worker_test.go": `package worker

import "testing"

func TestExecute(t *testing.T) {
	(&Runner{}).Execute()
}
`,
		"internal/broken/broken.go": "package broken\n\nfunc Execute(\n",
	})

	result, err := Find(context.Background(), Request{
		Root:       root,
		Terms:      []string{"worker"},
		Symbols:    []string{"Execute", "TestExecute", "Runner"},
		Paths:      []string{"internal"},
		MaxAnchors: 12,
		MaxMatches: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		path   string
		line   int
		role   string
		symbol string
	}{
		{path: "internal/worker/worker.go", line: 5, role: SyntaxRoleDefinition, symbol: "Execute"},
		{path: "internal/worker/worker.go", line: 8, role: SyntaxRoleCall, symbol: "Execute"},
		{path: "internal/worker/worker.go", line: 11, role: SyntaxRoleReference, symbol: "Execute"},
		{path: "internal/worker/worker_test.go", line: 5, role: SyntaxRoleDefinition, symbol: "TestExecute"},
		{path: "internal/worker/worker_test.go", line: 6, role: SyntaxRoleCall, symbol: "Execute"},
	} {
		anchor := findAnchor(result.Anchors, want.path, want.line)
		if anchor == nil || anchor.Syntax == nil || anchor.Syntax.Role != want.role || anchor.Syntax.Symbol != want.symbol || anchor.Syntax.Authority != syntaxAuthority {
			t.Fatalf("missing syntax evidence %+v in %+v", want, result.Anchors)
		}
	}
	if anchor := findAnchor(result.Anchors, "internal/worker/worker.go", 13); anchor == nil || anchor.Syntax != nil {
		t.Fatalf("comment must remain lexical-only: %+v", anchor)
	}
	for _, anchor := range result.Anchors {
		if anchor.Path == "internal/broken/broken.go" && anchor.Syntax != nil {
			t.Fatalf("partial parse must not produce syntax evidence: %+v", anchor)
		}
	}
	if result.Metrics.SyntaxFilesParsed != 2 || result.Metrics.SyntaxAnchors < 5 || result.Metrics.SyntaxParseErrors != 1 {
		t.Fatalf("unexpected syntax metrics: %+v", result.Metrics)
	}
}

func TestSyntaxSymbolsUseLastIdentifierPerSegment(t *testing.T) {
	symbols := syntaxSymbols([]string{"func (m *Module) Execute", "worker.Run", "Ask/RunPlayerJob", "100126"})
	for _, want := range []string{"Execute", "Run", "Ask", "RunPlayerJob"} {
		if _, ok := symbols[want]; !ok {
			t.Fatalf("missing %s in %#v", want, symbols)
		}
	}
	for _, unwanted := range []string{"func", "m", "Module", "worker"} {
		if _, ok := symbols[unwanted]; ok {
			t.Fatalf("unexpected %s in %#v", unwanted, symbols)
		}
	}
}

func TestFindSkipsOversizedGoFileForSyntax(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"large.go": "package large\nfunc Execute() {}\nvar payload = \"" + strings.Repeat("x", maxSyntaxFileBytes) + "\"\n",
	})
	result, err := Find(context.Background(), Request{Root: root, Symbols: []string{"Execute"}})
	if err != nil {
		t.Fatal(err)
	}
	anchor := findAnchor(result.Anchors, "large.go", 2)
	if anchor == nil || anchor.Syntax != nil {
		t.Fatalf("oversized file must remain lexical-only: %+v", result.Anchors)
	}
	if result.Metrics.SyntaxFilesParsed != 0 || result.Metrics.SyntaxFilesSkipped != 1 {
		t.Fatalf("unexpected syntax size budget metrics: %+v", result.Metrics)
	}
}

func TestEnrichSyntaxHonorsCanceledContext(t *testing.T) {
	root := makeRepo(t, map[string]string{"target.go": "package target\nfunc Execute() {}\n"})
	anchors := []Anchor{{Kind: "source", Path: "target.go", Line: 2, Text: "func Execute() {}", Groups: []string{"symbols"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	metrics := enrichSyntax(ctx, root, anchors, []string{"Execute"})
	if !metrics.budgetExceeded || metrics.filesParsed != 0 || metrics.filesSkipped != 1 || anchors[0].Syntax != nil {
		t.Fatalf("canceled syntax enrichment must stop without evidence: metrics=%+v anchor=%+v", metrics, anchors[0])
	}
}

func findAnchor(anchors []Anchor, path string, line int) *Anchor {
	for index := range anchors {
		if anchors[index].Path == path && anchors[index].Line == line {
			return &anchors[index]
		}
	}
	return nil
}
