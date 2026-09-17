package find

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const gbkReward = "id,\xbd\xb1\xc0\xf8\n"

func TestEncodingValidation(t *testing.T) {
	root := makeRepo(t, map[string]string{})
	for _, tc := range []struct{ input, want string }{{"", "auto"}, {"auto", "auto"}, {"utf-8", "utf-8"}, {" GBK ", "gbk"}, {"gb18030", "gb18030"}} {
		req, err := normalizeRequest(Request{Root: root, Terms: []string{"x"}, Encoding: tc.input})
		if err != nil || req.Encoding != tc.want {
			t.Fatalf("input=%q encoding=%q err=%v", tc.input, req.Encoding, err)
		}
	}
	_, err := Find(context.Background(), Request{Root: root, Terms: []string{"x"}, Encoding: "unknown"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestEncodingChineseConfiguration(t *testing.T) {
	requireRG(t)
	for _, tc := range []struct{ encoding, content string }{
		{"auto", "id,奖励\n"}, {"utf-8", "id,奖励\n"},
		{"gbk", gbkReward}, {"gb18030", gbkReward},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			root := makeRepo(t, map[string]string{"awards.csv": tc.content})
			result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}, Encoding: tc.encoding})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusCandidatesFound || len(result.Anchors) != 1 || result.Anchors[0].Text != "id,奖励" || result.Metrics.RGCalls != 1 || result.Metrics.EncodingRetries != 0 {
				t.Fatalf("result=%+v", result)
			}
			if !reflect.DeepEqual(result.Query.EncodingApplied, []string{tc.encoding}) {
				t.Fatalf("applied=%v", result.Query.EncodingApplied)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Result
			if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Query.Encoding != tc.encoding {
				t.Fatalf("encoding=%q err=%v", decoded.Query.Encoding, err)
			}
			if decoded.Metrics.EncodingRetries != 0 || !reflect.DeepEqual(decoded.Query.EncodingApplied, []string{tc.encoding}) {
				t.Fatalf("decoded=%+v", decoded.Query)
			}
		})
	}
}

func TestAutoRetriesInvalidUTF8CSVWithoutRescanningGo(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"server/a.go":   "package a\nconst Name = \"奖励\"\n",
		"balance/a.csv": gbkReward,
		"docs/notes.md": "arena notes\n",
		"ops/flag.yaml": "enabled: true\n",
	})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || result.Query.Encoding != "auto" || result.Metrics.EncodingRetries != 1 || result.Metrics.RGCalls != 2 {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(result.Query.EncodingApplied, []string{"auto", "gb18030"}) {
		t.Fatalf("applied=%v", result.Query.EncodingApplied)
	}
	if !hasPath(result.Anchors, "server/a.go") || !hasPath(result.Anchors, "balance/a.csv") {
		t.Fatalf("anchors=%+v", result.Anchors)
	}
	for _, a := range result.Anchors {
		if a.Path == "balance/a.csv" && a.Kind != "config" {
			t.Fatalf("csv kind=%s", a.Kind)
		}
		if a.Path == "docs/notes.md" || a.Path == "ops/flag.yaml" {
			t.Fatalf("unexpected fallback glob %s", a.Path)
		}
	}
}

func TestAutoFindsGBKCSVWithoutSourceHits(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"awards.csv": gbkReward})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || result.Query.Encoding != "auto" || result.Metrics.EncodingRetries != 1 || result.Metrics.RGCalls != 2 {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Anchors) != 1 || result.Anchors[0].Path != "awards.csv" || result.Anchors[0].Text != "id,奖励" {
		t.Fatalf("anchors=%+v", result.Anchors)
	}
}

func TestAutoDoesNotRetryValidUTF8ZeroHits(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"main.go": "package main\n", "awards.csv": "id,name\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"__codefind_missing_reward__"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(result.Query.EncodingApplied, []string{"auto"}) {
		t.Fatalf("applied=%v", result.Query.EncodingApplied)
	}
}

func TestAutoDoesNotRetryValidUTF8CSVWhenGoMisses(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"server/a.go": "package a\n", "balance/a.csv": "id,name\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestAutoRetriesInvalidUTF8TSV(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"awards.tsv": "id\t\xbd\xb1\xc0\xf8\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || result.Metrics.EncodingRetries != 1 || result.Metrics.RGCalls != 2 {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Anchors) != 1 || result.Anchors[0].Path != "awards.tsv" || result.Anchors[0].Kind != "config" || result.Anchors[0].Text != "id 奖励" {
		t.Fatalf("anchors=%+v", result.Anchors)
	}
}

func TestExplicitUTF8DoesNotFallbackOnGBKCSV(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"awards.csv": gbkReward})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}, Encoding: "utf-8"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 || result.Query.Encoding != "utf-8" {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(result.Query.EncodingApplied, []string{"utf-8"}) {
		t.Fatalf("applied=%v", result.Query.EncodingApplied)
	}
}

func TestBudgetExceededSkipsEncodingRetry(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"a.go":       "package a\nconst A = \"奖励\"\n",
		"b.go":       "package b\nconst B = \"奖励\"\n",
		"awards.csv": gbkReward,
	})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}, MaxAnchors: 1, MaxMatches: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusBudgetExceeded || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if hasPath(result.Anchors, "awards.csv") {
		t.Fatalf("retried after budget: %+v", result.Anchors)
	}
}

func TestUTF8BOMCSVDoesNotTriggerRetry(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"awards.csv": "\ufeffid,name\n"})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestEncodingProbeSkipsVendorAndDotDirs(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{
		"vendor/hidden.csv":     gbkReward,
		".git/hidden.csv":       gbkReward,
		"node_modules/hide.csv": gbkReward,
		"ok.go":                 "package ok\n",
	})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 0 || result.Metrics.RGCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestZeroHitUnknownDoesNotClaimAbsence(t *testing.T) {
	requireRG(t)
	root := makeRepo(t, map[string]string{"awards.csv": gbkReward})
	result, err := Find(context.Background(), Request{Root: root, Terms: []string{"不存在的玩法字段"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNoCandidates || result.Metrics.EncodingRetries != 1 {
		t.Fatalf("result=%+v", result)
	}
	joined := strings.ToLower(strings.Join(result.Unknowns, " "))
	if strings.Contains(joined, "不存在") || strings.Contains(joined, "穷尽所有编码") || strings.Contains(joined, "not implemented") {
		t.Fatalf("unknowns=%v", result.Unknowns)
	}
}
