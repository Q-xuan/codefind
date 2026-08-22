package find

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindSourceConsumerAndTest(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"cmd/app/config.go": `package app

func loadConfiguration() {
	config.LoadConfiguration()
}
`,
		"internal/config/load.go": `package config

// LoadConfiguration loads application settings.
func LoadConfiguration() {}
`,
		"internal/config/load_test.go": `package config

func TestLoadConfiguration(t *testing.T) {
	LoadConfiguration()
}
`,
		"docs/configuration.md":   "# Application configuration\n",
		"docs/vendor/ignored.md":  "configuration LoadConfiguration\n",
		"node_modules/ignored.go": "package ignored // configuration\n",
	})

	result, err := Find(context.Background(), Request{
		Root:    root,
		Terms:   []string{"configuration"},
		Symbols: []string{"LoadConfiguration", "TestLoadConfiguration"},
		Paths:   []string{"cmd", "internal", "docs"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound {
		t.Fatalf("status = %s", result.Status)
	}
	if result.Metrics.RGCalls != 2 || result.Metrics.AgentCalls != 1 {
		t.Fatalf("calls = %+v", result.Metrics)
	}
	for _, want := range []string{"cmd/app/config.go", "internal/config/load.go", "internal/config/load_test.go"} {
		if !hasPath(result.Anchors, want) {
			t.Fatalf("missing %s in %+v", want, result.Anchors)
		}
	}
	for _, anchor := range result.Anchors {
		if strings.Contains(anchor.Path, "vendor") || strings.Contains(anchor.Path, "node_modules") {
			t.Fatalf("excluded path returned: %s", anchor.Path)
		}
		if filepath.IsAbs(anchor.Path) || strings.HasPrefix(anchor.Path, "../") {
			t.Fatalf("unsafe path returned: %s", anchor.Path)
		}
	}
	if result.Anchors[0].Kind != "test" {
		t.Fatalf("first kind = %s, want test", result.Anchors[0].Kind)
	}
	if len(result.Anchors) < 3 || result.Anchors[1].Kind != "source" || result.Anchors[2].Kind != "consumer" {
		t.Fatalf("first projection round = %+v, want test/source/consumer", result.Anchors[:min(3, len(result.Anchors))])
	}
}

func TestFindTreatsTermAsLiteral(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"config.go": "package config\nfunc call() { ParseConfig(1) }\n",
	})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"ParseConfig("}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || !hasPath(result.Anchors, "config.go") {
		t.Fatalf("literal query failed: %+v", result)
	}
}

func TestFindZeroHitIsUnknown(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"main.go": "package main\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"__codefind_missing_literal__"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || len(result.Unknowns) != 1 || len(result.Anchors) != 0 {
		t.Fatalf("unexpected zero-hit result: %+v", result)
	}
	if strings.Contains(strings.ToLower(result.Unknowns[0]), "not implemented") {
		t.Fatalf("zero hit claimed not implemented: %q", result.Unknowns[0])
	}
}

func TestFindReportsToolUnavailable(t *testing.T) {
	root := makeRepo(t, map[string]string{"main.go": "package main\n"})
	t.Setenv("PATH", "")
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"main"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusToolUnavailable || len(result.Unknowns) != 1 || result.Metrics.RGCalls != 0 {
		t.Fatalf("unexpected tool-unavailable result: %+v", result)
	}
}

func TestFindDirectUsesOneRGCall(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"main.go": "package main\nfunc loadConfiguration() {}\n"})
	result, err := Find(context.Background(), Request{Root: root, Symbols: []string{"loadConfiguration"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || result.Metrics.RGCalls != 1 {
		t.Fatalf("unexpected direct result: %+v", result)
	}
}

func TestFindRejectsPathEscape(t *testing.T) {
	root := makeRepo(t, map[string]string{"main.go": "package main\n"})
	_, err := Find(context.Background(), Request{Root: root, Terms: []string{"main"}, Paths: []string{"../"}})
	if err == nil || !strings.Contains(err.Error(), "逃逸") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindTreatsDashPrefixedPathAsPath(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"--json/config.go": "package config\nfunc LoadConfiguration() {}\n"})
	result, err := Find(context.Background(), Request{
		Root:    root,
		Symbols: []string{"LoadConfiguration"},
		Paths:   []string{"--json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || !hasPath(result.Anchors, "--json/config.go") {
		t.Fatalf("dash-prefixed path was not searched literally: %+v", result)
	}
}

func TestFindLimitsProjectedAnchors(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"a.go": "package p\n// target\n",
		"b.go": "package p\n// target\n",
		"c.go": "package p\n// target\n",
	})
	result, err := Find(context.Background(), Request{
		Root:       root,
		Terms:      []string{"target"},
		MaxAnchors: 2,
		MaxMatches: 10,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Anchors) != 2 || !result.Metrics.Truncated {
		t.Fatalf("unexpected limit result: %+v", result)
	}
	if result.Status != StatusCandidatesFound {
		t.Fatalf("projection alone must not report budget exceeded: %+v", result)
	}
}

func TestFindReportsMatchBudgetExceeded(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"a.go": "package p\n// target\n",
		"b.go": "package p\n// target\n",
		"c.go": "package p\n// target\n",
	})
	result, err := Find(context.Background(), Request{
		Root:       root,
		Terms:      []string{"target"},
		MaxAnchors: 2,
		MaxMatches: 2,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExceeded || len(result.Anchors) != 2 || !result.Metrics.Truncated {
		t.Fatalf("unexpected budget result: %+v", result)
	}
}

func TestFindKeepsMatchedProductionBesideItsTest(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"service/a.go":      "package service\nfunc useTarget() { Target() }\n",
		"service/b.go":      "package service\nfunc useTarget() { Target() }\n",
		"service/b_test.go": "package service\nfunc TestTarget() { Target() }\n",
	})
	result, err := Find(context.Background(), Request{
		Root:       root,
		Symbols:    []string{"Target"},
		MaxAnchors: 2,
		MaxMatches: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasPath(result.Anchors, "service/b_test.go") || !hasPath(result.Anchors, "service/b.go") {
		t.Fatalf("paired source/test should survive projection: %+v", result.Anchors)
	}
}

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed")
	}
}

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hasPath(anchors []Anchor, path string) bool {
	for _, anchor := range anchors {
		if anchor.Path == path {
			return true
		}
	}
	return false
}
