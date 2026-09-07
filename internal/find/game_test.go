package find

import (
	"context"
	"strings"
	"testing"
)

// Synthetic gameplay fixtures: regression checks, not a real-project benchmark.
func TestGameDiscoveryAcrossContent(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"server/arena.go":          "package arena\nfunc ClaimArenaReward() {}\n",
		"server/arena_test.go":     "package arena\nfunc TestClaimArenaReward() {}\n",
		"shared/proto/arena.proto": "message ClaimArenaRewardRequest {}\n",
		"balance/rewards.csv":      "id,name\n100126,arena_reward\n",
		"operations/arena.yaml":    "arena_reward: true\n",
		"shared/proto/arena.md":    "# arena_reward protocol notes\n",
		"shared/proto/arena.pb.go": "package proto\n// ClaimArenaReward generated\n",
	})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"arena_reward", "100126"}, Symbols: []string{"ClaimArenaReward"}, MaxAnchors: 7})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"server/arena.go": "source", "server/arena_test.go": "test", "shared/proto/arena.proto": "protocol", "balance/rewards.csv": "config", "operations/arena.yaml": "config", "shared/proto/arena.md": "docs", "shared/proto/arena.pb.go": "generated"}
	for _, a := range result.Anchors {
		if kind, ok := want[a.Path]; ok && a.Kind == kind {
			delete(want, a.Path)
		}
	}
	if len(want) != 0 || result.Status != StatusCandidatesFound || result.Metrics.RGCalls != 2 {
		t.Fatalf("missing=%v result=%+v", want, result)
	}
}

func TestGameConfigIDPrefersWholeNumber(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"balance/a.csv": "id,name\n11001260,other\n", "balance/z.csv": "id,name\n100126,arena_reward\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"100126"}, MaxAnchors: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Anchors) != 1 || result.Anchors[0].Path != "balance/z.csv" {
		t.Fatalf("anchors=%+v", result.Anchors)
	}
}

func TestGameNumericBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text, pattern string
		want          bool
	}{
		{"100126,coins", "100126", true}, {"11001260", "100126", false},
		{"11001260,100126", "100126", true}, {"奖励：100126", "100126", true},
		{"１100126２", "100126", false}, {"arena_reward", "arena", false}, {"", "", false},
	} {
		if got := wholeNumberMatch(tc.text, tc.pattern); got != tc.want {
			t.Errorf("%q / %q: %v", tc.text, tc.pattern, got)
		}
	}
}

func TestGameClassificationUsesFileType(t *testing.T) {
	for _, tc := range []struct{ path, text, kind string }{
		{"tests/reward.yaml", "enabled: true", "config"},
		{"proto/readme.md", "# Protocol", "docs"},
		{"shared/proto/handler.go", "func Handle() {}", "source"},
		{"tests/arena.go", "func Check() {}", "test"},
		{"generated/reward.go", "var Reward = 1", "generated"},
	} {
		if got := classify(tc.path, tc.text); got != tc.kind {
			t.Errorf("%s: %s, want %s", tc.path, got, tc.kind)
		}
	}
}

func TestGameFollowUpStaysInScope(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"docs/arena.md": "arena_reward uses reward 100126\n", "balance/rewards.csv": "100126,coins\n", "outside/notes.md": "100126\n"})
	first, err := Find(context.Background(), Request{Root: root, Paths: []string{"docs"}, Terms: []string{"arena_reward"}})
	if err != nil || len(first.Anchors) != 1 || !strings.Contains(first.Anchors[0].Text, "100126") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	next, err := Find(context.Background(), Request{Root: root, Paths: []string{"balance"}, Terms: []string{"100126"}})
	if err != nil || len(next.Anchors) != 1 || next.Anchors[0].Path != "balance/rewards.csv" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}
