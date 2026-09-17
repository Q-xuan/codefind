package find

import (
	"context"
	"fmt"
	"math/rand"
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
	sheet, sst, e := workbookHints(context.Background(), filepath.Join(root, "design.xlsx"), []string{"COMMON"})
	if e != nil || sheet != 1 || sst != 0 {
		t.Fatalf("sheet=%d sst=%d error=%v", sheet, sst, e)
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
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Timeout: time.Second, Paths: []string{"design.xlsx", "z.xlsx"}})
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
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, MaxAnchors: 1, MaxMatches: 1, Paths: []string{"design.xlsx", "z.xlsx"}})
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
	if !namedHasPath(named, "design.xlsx") || named.WorkbookCoverage.NextPath == "" {
		t.Fatalf("named coverage=%+v", named.WorkbookCoverage)
	}
	if !hasDeferred(named, "skip.xlsx") {
		t.Fatalf("named hop lost siblings: %+v", named.WorkbookCoverage)
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
	if strings.Contains(joined, "不在工作簿") || strings.Contains(joined, "内容不存在") || strings.Contains(joined, "not in the workbook") {
		t.Fatalf("zero hit claimed absence: %q", joined)
	}
	if strings.Contains(joined, "表里没有") && !strings.Contains(joined, "不能写成表里没有") {
		t.Fatalf("zero hit claimed the field is missing: %q", joined)
	}
}

func TestXLSXDirectoryScansOneWorkbook(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "z.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	calls := 0
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := findWorkbooksWithScanner(context.Background(), req, Result{}, time.Now(), func(ctx context.Context, p string, patterns []string, emit func(string, string, string, string) error) error {
		calls++
		return emit("common", "B2", "cell", "Pvp")
	})
	if e != nil || calls != 1 || r.Status != StatusCandidatesFound || r.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("calls=%d r=%+v e=%v", calls, r, e)
	}
	if len(r.WorkbookCoverage.Files) != 2 || r.WorkbookCoverage.Files[1].Reason != reasonDeferred || r.WorkbookCoverage.Files[1].Status != "pending" {
		t.Fatalf("coverage=%+v", r.WorkbookCoverage)
	}
	if !r.WorkbookCoverage.DiscoveryComplete || r.WorkbookCoverage.NextPath == "" {
		t.Fatalf("coverage=%+v", r.WorkbookCoverage)
	}
	if r.WorkbookCoverage.Files[0].Size == 0 || r.WorkbookCoverage.Files[0].Score < 0 {
		t.Fatalf("missing rank fields: %+v", r.WorkbookCoverage.Files[0])
	}
}

func TestXLSXSharedStringHintBeatsSmallerFile(t *testing.T) {
	root := workbookFixture(t, nil)
	decoy, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "aaa.xlsx"), decoy, 0600); e != nil {
		t.Fatal(e)
	}
	target := workbookFixture(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst><si><t>木鱼每日获得功德上限</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="2"><c r="B2" t="s"><v>0</v></c></row></sheetData></worksheet>`,
	})
	targetBytes, e := os.ReadFile(filepath.Join(target, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "zzz.xlsx"), targetBytes, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"木鱼"}})
	if e != nil || r.Status != StatusCandidatesFound || r.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.WorkbookCoverage.Files[0].Path != "zzz.xlsx" || r.WorkbookCoverage.Files[0].Status != "complete" {
		t.Fatalf("selected=%+v", r.WorkbookCoverage)
	}
	for _, f := range r.WorkbookCoverage.Files[1:] {
		if f.Reason != reasonDeferred {
			t.Fatalf("expected deferred, got %+v", f)
		}
	}
	if !hasPath(r.Anchors, "zzz.xlsx") || hasPath(r.Anchors, "aaa.xlsx") || hasPath(r.Anchors, "design.xlsx") {
		t.Fatalf("anchors=%+v", r.Anchors)
	}
}

func TestXLSXNamedFilesStillScanAll(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "z.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	calls := 0
	req, e := normalizeRequest(Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Paths: []string{"design.xlsx", "z.xlsx"}})
	if e != nil {
		t.Fatal(e)
	}
	if req.dirScope {
		t.Fatal("named files must not be directory scope")
	}
	r, e := findWorkbooksWithScanner(context.Background(), req, Result{}, time.Now(), func(ctx context.Context, p string, patterns []string, emit func(string, string, string, string) error) error {
		calls++
		return emit("common", "B2", "cell", "Pvp")
	})
	if e != nil || calls != 2 || r.Status != StatusCandidatesFound {
		t.Fatalf("calls=%d r=%+v e=%v", calls, r, e)
	}
	for _, f := range r.WorkbookCoverage.Files {
		if f.Reason == reasonDeferred {
			t.Fatalf("named files should not defer: %+v", r.WorkbookCoverage)
		}
	}
}

func TestXLSXDeferredZeroHitIsUnknown(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "z.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"木鱼上限"}})
	if e != nil || r.Status != StatusNoCandidates || len(r.Anchors) != 0 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("scanned=%d", r.Metrics.XLSXFilesScanned)
	}
	joined := strings.Join(r.Unknowns, "\n")
	if !strings.Contains(joined, "unknown") || !strings.Contains(joined, "deferred") {
		t.Fatalf("unknowns=%q", joined)
	}
	if strings.Contains(joined, "不在工作簿") || strings.Contains(joined, "内容不存在") {
		t.Fatalf("claimed absence: %q", joined)
	}
}

func TestXLSXTieDoesNotPreferTinyFile(t *testing.T) {
	tinyRoot := workbookFixture(t, map[string]string{"xl/sharedStrings.xml": `<sst><si><t>木鱼</t></si></sst>`})
	pad := make([]byte, 700<<10)
	if _, e := rand.New(rand.NewSource(1)).Read(pad); e != nil {
		t.Fatal(e)
	}
	midRoot := workbookFixture(t, map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>木鱼</t></si></sst>`,
		"xl/padding.bin":       string(pad),
	})
	root := t.TempDir()
	tiny, e := os.ReadFile(filepath.Join(tinyRoot, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	mid, e := os.ReadFile(filepath.Join(midRoot, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "aaa.xlsx"), tiny, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "zzz.xlsx"), mid, 0600); e != nil {
		t.Fatal(e)
	}
	if int64(len(mid)) < 512<<10 {
		t.Fatalf("mid fixture too small: %d", len(mid))
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"木鱼"}})
	if e != nil || r.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.WorkbookCoverage.Files[0].Path != "zzz.xlsx" {
		t.Fatalf("tiny file won a tied term: %+v", r.WorkbookCoverage)
	}
	if r.WorkbookCoverage.Files[0].Score != r.WorkbookCoverage.Files[1].Score {
		t.Fatalf("expected tied scores, got %+v", r.WorkbookCoverage)
	}
	if r.WorkbookCoverage.Files[0].Size <= r.WorkbookCoverage.Files[1].Size {
		t.Fatalf("expected mid-size first: %+v", r.WorkbookCoverage)
	}
}

func TestXLSXNamedPathKeepsDeferredQueue(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "mid.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "zzz.xlsx"), data, 0600); e != nil {
		t.Fatal(e)
	}
	first, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || first.WorkbookCoverage.NextPath == "" {
		t.Fatalf("first=%+v e=%v", first, e)
	}
	hop, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Paths: []string{first.WorkbookCoverage.NextPath}})
	if e != nil || hop.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("hop=%+v e=%v", hop, e)
	}
	if len(hop.WorkbookCoverage.Files) != 3 {
		t.Fatalf("hop dropped the queue: %+v", hop.WorkbookCoverage)
	}
	scanned := 0
	deferred := 0
	for _, f := range hop.WorkbookCoverage.Files {
		if f.Status == "complete" {
			scanned++
			if f.Path != first.WorkbookCoverage.NextPath {
				t.Fatalf("hop scanned %s, want %s", f.Path, first.WorkbookCoverage.NextPath)
			}
		}
		if f.Reason == reasonDeferred {
			deferred++
		}
	}
	if scanned != 1 || deferred != 2 || hop.WorkbookCoverage.NextPath == "" || hop.WorkbookCoverage.NextPath == first.WorkbookCoverage.NextPath {
		t.Fatalf("hop=%+v", hop.WorkbookCoverage)
	}
}

func namedHasPath(r Result, path string) bool {
	if r.WorkbookCoverage == nil {
		return false
	}
	for _, f := range r.WorkbookCoverage.Files {
		if f.Path == path && f.Status == "complete" {
			return true
		}
	}
	return false
}

func hasDeferred(r Result, path string) bool {
	if r.WorkbookCoverage == nil {
		return false
	}
	for _, f := range r.WorkbookCoverage.Files {
		if f.Path == path && f.Reason == reasonDeferred {
			return true
		}
	}
	return false
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
