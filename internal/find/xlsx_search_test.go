package find

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestXLSXMetadataOrdering(t *testing.T) {
	root := workbookFixture(t, nil)
	bytes, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "common.xlsx"), bytes, 0600); e != nil {
		t.Fatal(e)
	}
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"common"}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := findWorkbooksWithScanner(context.Background(), req, Result{}, time.Now(), func(ctx context.Context, p string, patterns []string, emit func(string, string, string, string) error) error {
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.WorkbookCoverage.Files) != 2 || r.WorkbookCoverage.Files[0].Path != "common.xlsx" {
		t.Fatalf("coverage=%+v", r.WorkbookCoverage)
	}
	score, e := workbookNameScore(context.Background(), filepath.Join(root, "design.xlsx"), []string{"COMMON"})
	if e != nil || score != 1 {
		t.Fatalf("score=%d error=%v", score, e)
	}
}

func TestXLSXFileTimeoutContinues(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "z.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	r, e := findWorkbooksWithScanner(context.Background(), req, Result{}, time.Now(), func(ctx context.Context, p string, patterns []string, emit func(string, string, string, string) error) error {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return emit("common", "B2", "cell", "Pvp")
	})
	if e != nil || calls != 2 || r.Status != StatusBudgetExceeded || len(r.Anchors) != 1 {
		t.Fatalf("calls=%d r=%+v e=%v", calls, r, e)
	}
	if r.WorkbookCoverage.Files[0].Reason != "file_timeout" || r.WorkbookCoverage.Files[1].Status != "complete" {
		t.Fatal(r.WorkbookCoverage)
	}
}

func TestXLSXPendingAfterMatchLimit(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "z.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, MaxAnchors: 1, MaxMatches: 1})
	if e != nil {
		t.Fatal(e)
	}
	c := r.WorkbookCoverage
	if !c.DiscoveryComplete || c.Files[0].Reason != "match_limit" || c.Files[1].Status != "pending" || c.Files[1].Reason != "match_limit" {
		t.Fatalf("coverage=%+v", c)
	}
}

func TestXLSXSheetOrdering(t *testing.T) {
	root := workbookFixture(t, nil)
	first := ""
	err := scanWorkbook(context.Background(), filepath.Join(root, "design.xlsx"), []string{"items"}, func(sheet, cell, source, text string) error {
		if first == "" {
			first = sheet
		}
		return nil
	})
	if err != nil || first != "items" {
		t.Fatalf("first=%s err=%v", first, err)
	}
}

func TestXLSXDiscoveryLimitReported(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < maxWorkbooks; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("%02d.xlsx", i)), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil {
		t.Fatal(e)
	}
	files, complete, e := discoverWorkbooks(context.Background(), req)
	if e != nil || complete || len(files) != maxWorkbooks {
		t.Fatalf("files=%d complete=%v err=%v", len(files), complete, e)
	}
}

func TestXLSXPathNamesAndExcludesWorkbook(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "skip.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	named, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Paths: []string{"design.xlsx"}})
	if e != nil || named.Status != StatusCandidatesFound || named.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("named=%+v e=%v", named, e)
	}
	if len(named.WorkbookCoverage.Files) != 1 || named.WorkbookCoverage.Files[0].Path != "design.xlsx" {
		t.Fatalf("named coverage=%+v", named.WorkbookCoverage)
	}
	excluded, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Paths: []string{"!skip.xlsx"}})
	if e != nil || excluded.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("excluded=%+v e=%v", excluded, e)
	}
	if len(excluded.WorkbookCoverage.Files) != 1 || excluded.WorkbookCoverage.Files[0].Path != "design.xlsx" {
		t.Fatalf("excluded coverage=%+v", excluded.WorkbookCoverage)
	}
	if !contains(excluded.Query.Paths, ".") || !contains(excluded.Query.Paths, "!skip.xlsx") {
		t.Fatalf("query.paths=%v", excluded.Query.Paths)
	}
}

func TestXLSXZeroHitIsUnknownNotAbsence(t *testing.T) {
	root := workbookFixture(t, nil)
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"木鱼上限"}})
	if e != nil || r.Status != StatusNoCandidates || len(r.Anchors) != 0 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	joined := strings.Join(r.Unknowns, "\n")
	if !strings.Contains(joined, "unknown") {
		t.Fatalf("unknowns missing unknown: %q", joined)
	}
	for _, bad := range []string{"表里没有", "不在工作簿", "内容不存在", "not in the workbook"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("zero hit claimed absence: %q", joined)
		}
	}
}

func TestXLSXMetadataFailureDoesNotExcludeFile(t *testing.T) {
	root := workbookFixture(t, map[string]string{"xl/workbook.xml": "<workbook>"})
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	r, e := findWorkbooksWithScanner(context.Background(), req, Result{}, time.Now(), func(context.Context, string, []string, func(string, string, string, string) error) error {
		calls++
		return nil
	})
	if e != nil || calls != 1 || r.WorkbookCoverage.Files[0].MetadataStatus != "unavailable" {
		t.Fatalf("calls=%d r=%+v e=%v", calls, r, e)
	}
}
