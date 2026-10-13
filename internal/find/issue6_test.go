package find

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXLSXBatchAndResumeDoesNotRepeatOrClaimFullScan(t *testing.T) {
	root := workbookFixture(t, nil)
	data, e := os.ReadFile(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"a.xlsx", "b.xlsx"} {
		if e := os.WriteFile(filepath.Join(root, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	one, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || one.Metrics.XLSXFilesScanned != 1 || one.WorkbookCoverage.ContentComplete {
		t.Fatalf("default scan=%+v e=%v", one.WorkbookCoverage, e)
	}
	batch, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, MaxFiles: 2})
	if e != nil || batch.Metrics.XLSXFilesScanned != 2 || batch.WorkbookCoverage.ContentComplete {
		t.Fatalf("batch=%+v e=%v", batch.WorkbookCoverage, e)
	}
	deferred := 0
	done := map[string]bool{}
	for _, f := range batch.WorkbookCoverage.Files {
		if f.Status == "complete" && f.Reason == "" {
			done[f.Path] = true
		}
		if f.Reason == reasonDeferred {
			deferred++
		}
	}
	if len(done) != 2 || deferred != 1 || batch.WorkbookCoverage.ResumeCursor == "" || batch.WorkbookCoverage.NextCommand == "" {
		t.Fatalf("batch coverage=%+v", batch.WorkbookCoverage)
	}
	joined := strings.Join(batch.Unknowns, "\n")
	if !strings.Contains(joined, "不能当成全目录已扫描") || strings.Contains(joined, "全目录已经扫描") {
		t.Fatalf("unknowns=%q", joined)
	}
	if !strings.Contains(batch.WorkbookCoverage.NextCommand, "--cursor") || !strings.Contains(batch.WorkbookCoverage.NextCommand, "--max-files") {
		t.Fatal(batch.WorkbookCoverage.NextCommand)
	}
	resume, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, MaxFiles: 2, Cursor: batch.WorkbookCoverage.ResumeCursor})
	if e != nil || resume.Metrics.XLSXFilesScanned != 1 || !resume.WorkbookCoverage.ContentComplete {
		t.Fatalf("resume=%+v e=%v", resume.WorkbookCoverage, e)
	}
	for _, f := range resume.WorkbookCoverage.Files {
		if done[f.Path] {
			if f.Reason != "cursor_skipped" || f.Status != "complete" {
				t.Fatalf("rescanned %s: %+v", f.Path, f)
			}
			for _, a := range resume.Anchors {
				if a.Path == f.Path {
					t.Fatalf("anchor repeated completed file %s", f.Path)
				}
			}
		}
	}
	if _, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Cursor: "nope"}); e == nil || !strings.Contains(e.Error(), "cursor") {
		t.Fatal(e)
	}
}

func TestXLSXReadErrorDoesNotBlockBatch(t *testing.T) {
	good := workbookFixture(t, nil)
	bad := workbookFixture(t, map[string]string{"xl/sharedStrings.xml": "<sst>"})
	root := t.TempDir()
	goodBytes, e := os.ReadFile(filepath.Join(good, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	badBytes, e := os.ReadFile(filepath.Join(bad, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "good.xlsx"), goodBytes, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "bad.xlsx"), badBytes, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, MaxFiles: 2})
	if e != nil || r.Metrics.XLSXFilesScanned != 2 || len(r.Anchors) == 0 {
		t.Fatalf("r=%+v e=%v", r.WorkbookCoverage, e)
	}
	failed := false
	completed := false
	for _, f := range r.WorkbookCoverage.Files {
		if f.Reason == "read_error" {
			failed = true
		}
		if f.Status == "complete" && f.Reason == "" {
			completed = true
		}
	}
	if !failed || !completed || !strings.Contains(strings.Join(r.Unknowns, "\n"), "没有挡住") {
		t.Fatalf("coverage=%+v unknowns=%v", r.WorkbookCoverage, r.Unknowns)
	}
}

func TestXLSXSheetLimitKeepsReadableSheet(t *testing.T) {
	if maxWorkbookBytes != 32<<20 || maxWorkbookXMLBytes != 64<<20 {
		t.Fatalf("global limits are not the production ceilings: %d %d", maxWorkbookBytes, maxWorkbookXMLBytes)
	}
	big := strings.Repeat("x", 8000)
	root := workbookFixture(t, map[string]string{
		"xl/worksheets/sheet2.xml": `<worksheet><sheetData><row r="2"><c r="B2" t="inlineStr"><is><t>` + big + `</t></is></c></row></sheetData></worksheet>`,
	})
	z, e := zip.OpenReader(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	var need, sheet2 uint64
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, "sheet2.xml") {
			sheet2 = f.UncompressedSize64
			continue
		}
		need += f.UncompressedSize64
	}
	z.Close()
	if sheet2 <= 64 {
		t.Fatalf("sheet2=%d", sheet2)
	}
	old := maxWorkbookXMLBytes
	maxWorkbookXMLBytes = int64(need) + 64
	t.Cleanup(func() { maxWorkbookXMLBytes = old })
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || r.Status != StatusBudgetExceeded || len(r.Anchors) == 0 {
		t.Fatalf("r=%+v anchors=%d e=%v", r.WorkbookCoverage, len(r.Anchors), e)
	}
	file := r.WorkbookCoverage.Files[0]
	if file.Status != "partial" || file.Reason != "sheet_limit" || r.WorkbookCoverage.ContentComplete {
		t.Fatalf("file=%+v", file)
	}
	var good, bad *SheetScan
	for i := range file.Sheets {
		s := &file.Sheets[i]
		if s.Status == "complete" {
			good = s
		}
		if s.Status == "partial" {
			bad = s
		}
	}
	if good == nil || good.Name != "common" || bad == nil {
		t.Fatalf("sheets=%+v", file.Sheets)
	}
	if bad.LimitKind != "xml_part" || bad.Entry == "" || !strings.Contains(bad.Entry, "sheet2.xml") || bad.ActualBytes <= bad.LimitBytes || bad.BudgetBytes != maxWorkbookXMLBytes {
		t.Fatalf("bad=%+v budget=%d", bad, maxWorkbookXMLBytes)
	}
	if file.LimitKind != "xml_part" || file.Sheet == "" || file.ActualBytes <= file.LimitBytes {
		t.Fatalf("file diagnostic=%+v", file)
	}
	found := false
	for _, a := range r.Anchors {
		if a.Workbook != nil && a.Workbook.Sheet == "common" && a.Workbook.Cell == "B2" {
			found = true
		}
		if a.Workbook != nil && a.Workbook.Sheet == bad.Name {
			t.Fatalf("anchor from limited sheet: %+v", a)
		}
	}
	if !found {
		t.Fatalf("missing readable anchor: %+v", r.Anchors)
	}
}

func TestXLSXFileSizeLimitIsPrecise(t *testing.T) {
	if maxWorkbookBytes != 32<<20 {
		t.Fatal(maxWorkbookBytes)
	}
	root := workbookFixture(t, nil)
	info, e := os.Stat(filepath.Join(root, "design.xlsx"))
	if e != nil {
		t.Fatal(e)
	}
	old := maxWorkbookBytes
	maxWorkbookBytes = 8
	t.Cleanup(func() { maxWorkbookBytes = old })
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || r.Status != StatusBudgetExceeded || len(r.Anchors) != 0 || r.WorkbookCoverage.ContentComplete {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	f := r.WorkbookCoverage.Files[0]
	if f.Status != "partial" || f.Reason != "file_size" || f.LimitKind != "file_size" || f.LimitBytes != 8 || f.ActualBytes != info.Size() || f.BudgetBytes != 8 {
		t.Fatalf("file=%+v size=%d", f, info.Size())
	}
}

func TestXLSXExactMatchIsWholeCell(t *testing.T) {
	root := workbookFixture(t, map[string]string{
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="2"><c r="B2" t="inlineStr"><is><t>Pvp</t></is></c><c r="C2" t="inlineStr"><is><t>PvpExtra</t></is></c><c r="E2" t="inlineStr"><is><t>每日木鱼产出功德上限</t></is></c></row></sheetData></worksheet>`,
	})
	exact, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Match: "exact"})
	if e != nil || exact.Status != StatusCandidatesFound || exact.Query.Match != "exact" {
		t.Fatalf("exact=%+v e=%v", exact, e)
	}
	if len(exact.Anchors) != 1 || exact.Anchors[0].Workbook.Cell != "B2" || strings.Join(exact.Anchors[0].MatchedTerms, ",") != "Pvp" {
		t.Fatalf("anchors=%+v", exact.Anchors)
	}
	sub, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || sub.Query.Match != "substring" || len(sub.Anchors) < 3 {
		t.Fatalf("sub=%+v e=%v", sub, e)
	}
	cells := map[string]bool{}
	for _, a := range sub.Anchors {
		cells[a.Workbook.Cell] = true
		if len(a.MatchedTerms) != 1 || a.MatchedTerms[0] != "Pvp" {
			t.Fatalf("matched=%v text=%s", a.MatchedTerms, a.Text)
		}
	}
	if !cells["B2"] || !cells["C2"] || !cells["D2"] {
		t.Fatalf("substring cells=%v", cells)
	}
	for _, match := range []string{"", "exact"} {
		miss, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"木鱼上限"}, Match: match})
		if e != nil || miss.Status != StatusNoCandidates || len(miss.Anchors) != 0 {
			t.Fatalf("match=%q miss=%+v e=%v", match, miss, e)
		}
		if !strings.Contains(strings.Join(miss.Unknowns, "\n"), "unknown") {
			t.Fatal(miss.Unknowns)
		}
	}
}

func TestXLSXSheetFilterSkipsOthers(t *testing.T) {
	root := workbookFixture(t, nil)
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"挑战券"}, Sheets: []string{"items"}})
	if e != nil || r.Status != StatusCandidatesFound || len(r.Anchors) != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.Anchors[0].Workbook.Sheet != "items" {
		t.Fatal(r.Anchors[0])
	}
	skipped := false
	for _, s := range r.WorkbookCoverage.Files[0].Sheets {
		if s.Name == "common" && s.Reason == "not_requested" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("sheets=%+v", r.WorkbookCoverage.Files[0].Sheets)
	}
	miss, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}, Sheets: []string{"items"}})
	if e != nil || miss.Status != StatusNoCandidates || len(miss.Anchors) != 0 {
		t.Fatalf("miss=%+v e=%v", miss, e)
	}
}

func TestReadRangeContinuationCoversWithoutOverlap(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<worksheet><sheetData>`)
	for _, ref := range []string{"A1", "B1", "C1", "A2", "B2", "C2"} {
		body.WriteString(`<row><c r="` + ref + `" t="inlineStr"><is><t>` + ref + `</t></is></c></row>`)
	}
	body.WriteString(`</sheetData></worksheet>`)
	root := workbookFixture(t, map[string]string{"xl/worksheets/sheet1.xml": body.String()})
	req := ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1:C2", MaxCells: 2}
	seen := map[string]int{}
	var first ReadResult
	for page := 0; page < 5; page++ {
		r, e := ReadWorkbook(context.Background(), req)
		if e != nil {
			t.Fatal(e)
		}
		if page == 0 {
			first = r
			if !r.Coverage.ParseComplete || !r.Coverage.CellTruncated || r.Coverage.CharTruncated || r.Coverage.RequestedCells != 6 || r.Coverage.ReturnedCells != 2 || r.Status != StatusBudgetExceeded {
				t.Fatalf("first=%+v", r.Coverage)
			}
			if r.Coverage.ResumeCursor == "" || len(r.Coverage.NextRanges) == 0 || r.Coverage.LastCell == "" {
				t.Fatalf("continuation=%+v", r.Coverage)
			}
		}
		if !r.Coverage.ParseComplete {
			t.Fatalf("parse incomplete page %d: %+v", page, r.Coverage)
		}
		for _, c := range r.Cells {
			seen[c.Cell]++
			if c.Value == nil || *c.Value != c.Cell {
				t.Fatalf("cell=%+v", c)
			}
		}
		if r.Coverage.ResumeCursor == "" {
			if r.Coverage.CellTruncated || r.Coverage.ReturnedCells != r.Coverage.RequestedCells {
				t.Fatalf("final page=%+v", r.Coverage)
			}
			break
		}
		req.Cursor = r.Coverage.ResumeCursor
		if page == 4 {
			t.Fatal("continuation did not finish")
		}
	}
	if len(seen) != 6 {
		t.Fatalf("seen=%v", seen)
	}
	for ref, n := range seen {
		if n != 1 {
			t.Fatalf("%s repeated %d", ref, n)
		}
	}
	union := map[string]bool{}
	for _, c := range first.Cells {
		union[c.Cell] = true
	}
	for _, next := range first.Coverage.NextRanges {
		r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: next, MaxCells: 96})
		if e != nil || r.Coverage.Truncated {
			t.Fatalf("range %s r=%+v e=%v", next, r.Coverage, e)
		}
		for _, c := range r.Cells {
			if union[c.Cell] {
				t.Fatalf("next range %s overlapped %s", next, c.Cell)
			}
			union[c.Cell] = true
		}
	}
	if len(union) != 6 {
		t.Fatalf("union=%v", union)
	}
	chars, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1:C2", MaxChars: 1})
	if e != nil || !chars.Coverage.CharTruncated || chars.Coverage.CellTruncated || !chars.Coverage.ParseComplete {
		t.Fatalf("chars=%+v e=%v", chars.Coverage, e)
	}
}

func TestReadSharedFormulaProjection(t *testing.T) {
	sheet := `<worksheet><sheetData>` +
		`<row r="1"><c r="A1"><f t="shared" ref="A1:A2" si="0">B1+1</f><v>1</v></c>` +
		`<c r="B1"><f t="shared" ref="B1:B2" si="1">$C$1+D1</f><v>3</v></c>` +
		`<c r="C1"><f t="shared" ref="C1:C2" si="2">Other!E1+'费用'!$F$1</f><v>5</v></c>` +
		`<c r="D1"><f t="shared" ref="D1:D2" si="3">SUM([1]Other!A1)</f><v>7</v></c></row>` +
		`<row r="2"><c r="A2"><f t="shared" si="0"/><v>2</v></c>` +
		`<c r="B2"><f t="shared" si="1"/><v>4</v></c>` +
		`<c r="C2"><f t="shared" si="2"/><v>6</v></c>` +
		`<c r="D2"><f t="shared" si="3"/><v>8</v></c></row>` +
		`</sheetData></worksheet>`
	root := workbookFixture(t, map[string]string{
		"xl/worksheets/sheet1.xml":            sheet,
		"xl/worksheets/_rels/sheet1.xml.rels": `<Relationships></Relationships>`,
	})
	r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A2:D2"})
	if e != nil || len(r.Cells) != 4 || r.Coverage.CellsScanned != 4 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	by := map[string]ReadCell{}
	for _, c := range r.Cells {
		by[c.Cell] = c
	}
	check := func(ref, anchor, expr, resolved, status string) {
		t.Helper()
		c := by[ref]
		if c.Formula == nil || c.Formula.Expression != "" || c.Formula.SharedAnchor != anchor || c.Formula.AnchorExpression != expr || c.Formula.ResolveStatus != status {
			t.Fatalf("%s formula=%+v", ref, c.Formula)
		}
		if status == "mechanical" && c.Formula.ResolvedExpression != resolved {
			t.Fatalf("%s resolved=%q", ref, c.Formula.ResolvedExpression)
		}
		if status == "unknown" && c.Formula.ResolvedExpression != "" {
			t.Fatalf("%s guessed %q", ref, c.Formula.ResolvedExpression)
		}
		if c.CachedValue == nil || *c.CachedValue == resolved {
			t.Fatalf("%s cached must not be the expansion: %+v", ref, c)
		}
	}
	check("A2", "A1", "B1+1", "B2+1", "mechanical")
	check("B2", "B1", "$C$1+D1", "$C$1+D2", "mechanical")
	check("C2", "C1", "Other!E1+'费用'!$F$1", "Other!E2+'费用'!$F$1", "mechanical")
	check("D2", "D1", "SUM([1]Other!A1)", "", "unknown")
	joined := strings.Join(r.Unknowns, "\n")
	if !strings.Contains(joined, "没有重新计算") || !strings.Contains(joined, "unknown") {
		t.Fatal(joined)
	}
	only, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A2"})
	if e != nil || only.Coverage.CellsScanned != 1 || len(only.Cells) != 1 || only.Cells[0].Formula.ResolvedExpression != "B2+1" || only.Cells[0].Formula.SharedAnchor != "A1" {
		t.Fatalf("outside anchor=%+v scanned=%d e=%v", only.Cells, only.Coverage.CellsScanned, e)
	}
}
