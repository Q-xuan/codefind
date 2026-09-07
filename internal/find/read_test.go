package find

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRangeFormulaComments(t *testing.T) {
	root := workbookFixture(t, nil)
	r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "B2:D2"})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Cells) != 3 || r.Status != "read_complete" || !r.Coverage.ParseComplete {
		t.Fatalf("r=%+v", r)
	}
	if r.Cells[1].Formula == nil || r.Cells[1].Formula.Expression != "1+1" || r.Cells[1].CachedValue == nil || *r.Cells[1].CachedValue != "2" || r.Cells[1].Value != nil {
		t.Fatalf("cell=%+v", r.Cells[1])
	}
	if r.Cells[2].Comment == nil {
		t.Fatal("missing comment")
	}
}

func TestReadBudgets(t *testing.T) {
	root := workbookFixture(t, nil)
	for _, req := range []ReadRequest{
		{Root: root, File: "design.xlsx", Sheet: "common", Range: "B2:D2", MaxCells: 1},
		{Root: root, File: "design.xlsx", Sheet: "common", Range: "B2:D2", MaxChars: 2},
	} {
		r, e := ReadWorkbook(context.Background(), req)
		if e != nil || r.Status != StatusBudgetExceeded || !r.Coverage.Truncated {
			t.Fatalf("r=%+v e=%v", r, e)
		}
	}
}

func TestReadUncachedAndSharedFormulas(t *testing.T) {
	root := workbookFixture(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1"><f>SUM(B1:C1)</f></c><c r="B1"><f t="shared" si="0"/><v>2</v></c></row></sheetData></worksheet>`})
	r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1:B1"})
	if e != nil || len(r.Cells) != 2 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if r.Cells[0].Formula == nil || r.Cells[0].CachedValue != nil || r.Cells[1].Formula.Type != "shared" || r.Cells[1].Formula.SharedIndex != "0" {
		t.Fatalf("cells=%+v", r.Cells)
	}
}

func TestReadTitleLooksBelow(t *testing.T) {
	root := workbookFixture(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="C11" t="inlineStr"><is><t>common</t></is></c><c r="G12" t="inlineStr"><is><t>参数2</t></is></c><c r="D16" t="inlineStr"><is><t>Chat</t></is></c><c r="G16"><v>50</v></c><c r="G17" t="inlineStr"><is><t>Maximum characters</t></is></c></row></sheetData></worksheet>`})
	r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Anchor: "C11", Fields: []string{"参数2"}})
	if e != nil || r.Strategy != "below_headers" || r.FallbackReason != "no_field_columns_above" {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	for _, ref := range []string{"C11", "G12", "D16", "G16", "G17"} {
		found := false
		for _, cell := range r.Cells {
			if cell.Cell == ref {
				found = true
			}
		}
		if !found {
			t.Fatal("missing", ref)
		}
	}
}

func TestReadBelowHeaderBoundariesAndAmbiguity(t *testing.T) {
	req := ReadRequest{Anchor: "C11", Fields: []string{"参数2"}, Strategy: "adaptive"}
	values := map[string]string{"C11": "common", "G12": "参数2", "H12": "参数2", "G16": "50", "H16": "60", "J20": "参数2", "G21": "outside"}
	refs, fields, strategy, _ := selectReadCells(req, readMap(values, nil), nil, nil)
	if strategy != "below_headers" || len(fields) != 2 || contains(refs, "J20") || contains(refs, "G21") {
		t.Fatalf("refs=%v fields=%v strategy=%s", refs, fields, strategy)
	}
}

func TestReadValidation(t *testing.T) {
	root := workbookFixture(t, nil)
	for _, req := range []ReadRequest{
		{Root: root, File: "../outside.xlsx", Sheet: "common", Range: "A1"},
		{Root: root, File: "design.xlsx", Sheet: "missing", Range: "A1"},
		{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1:XFD100"},
		{Root: root, File: "design.xlsx", Sheet: "common", Range: "C3:A1"},
		{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1", Anchor: "A1"},
		{Root: root, File: "design.xlsx", Sheet: "common", Anchor: "A1"},
	} {
		_, e := ReadWorkbook(context.Background(), req)
		if !errors.Is(e, ErrInvalidRequest) {
			t.Fatalf("req=%+v err=%v", req, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := ReadWorkbook(ctx, ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "A1"})
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestReadSelectionCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	refs, _, _, _ := selectReadCellsContext(ctx, ReadRequest{Strategy: "range"}, readMap(map[string]string{"A1": "value"}, nil), nil, []rect{{1, 1, 1, 1}})
	if len(refs) != 0 {
		t.Fatal(refs)
	}
}

func TestReadRejectsSymlinkEscape(t *testing.T) {
	outside := workbookFixture(t, nil)
	root := t.TempDir()
	if e := os.Symlink(filepath.Join(outside, "design.xlsx"), filepath.Join(root, "escape.xlsx")); e != nil {
		t.Skipf("symlink unavailable: %v", e)
	}
	_, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "escape.xlsx", Sheet: "common", Range: "A1"})
	if !errors.Is(e, ErrInvalidRequest) {
		t.Fatalf("err=%v", e)
	}
}

func TestReadMergedAnchorOutsideRange(t *testing.T) {
	root := workbookFixture(t, map[string]string{"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>Header</t></is></c></row></sheetData><mergeCells><mergeCell ref="A1:D1"/></mergeCells></worksheet>`})
	r, e := ReadWorkbook(context.Background(), ReadRequest{Root: root, File: "design.xlsx", Sheet: "common", Range: "B1:C1"})
	if e != nil || len(r.Merges) != 1 || len(r.Unknowns) < 2 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
}

func readMap(values map[string]string, comments map[string]string) map[string]ReadCell {
	cells := map[string]ReadCell{}
	for ref, value := range values {
		v := value
		cells[ref] = ReadCell{Cell: ref, Value: &v}
	}
	for ref, value := range comments {
		v := value
		c := cells[ref]
		c.Cell = ref
		c.Comment = &v
		cells[ref] = c
	}
	return cells
}

func TestReadStrategiesEvaluatedLayouts(t *testing.T) {
	for _, tc := range []struct {
		name, anchor     string
		values, comments map[string]string
		want             []string
		strategy         string
	}{
		{"nearby", "C8", map[string]string{"C8": "Feature", "D2": "参数1", "D8": "5"}, map[string]string{"D8": "Daily attempts"}, []string{"D2", "D8"}, "structure"},
		{"parallel", "D20", map[string]string{"D20": "Feature", "E7": "参数1", "E20": "5", "T20": "Feature", "U7": "参数1", "U20": "Daily attempts"}, nil, []string{"E7", "E20", "T20", "U7", "U20"}, "structure"},
		{"offset", "D35", map[string]string{"D35": "Feature", "E18": "param1", "E35": "5"}, nil, []string{"E18", "E35"}, "structure"},
		{"merged", "C8", map[string]string{"C8": "Feature", "D2": "参数1", "D8": "5"}, nil, []string{"D2", "D8"}, "structure"},
		{"ambiguous", "C8", map[string]string{"C8": "Feature", "D2": "参数1", "E2": "参数1", "D8": "5", "E8": "9"}, nil, []string{"D2", "E2", "D8", "E8"}, "structure"},
		{"cross_sheet_unresolved", "C8", map[string]string{"C8": "Feature", "D2": "参数1", "D8": "5"}, nil, []string{"D2", "D8"}, "structure"},
		{"left", "C8", map[string]string{"C8": "Feature", "B2": "参数1", "B8": "5"}, nil, []string{"B2", "B8"}, "row_headers"},
		{"freeform", "D20", map[string]string{"D20": "Feature", "E20": "备注", "F20": "Daily attempts"}, nil, []string{"E20", "F20"}, "row_headers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ReadRequest{Anchor: tc.anchor, Fields: []string{"参数1", "param1"}, Strategy: "adaptive"}
			refs, fields, strategy, reason := selectReadCells(req, readMap(tc.values, tc.comments), []rect{{2, 4, 2, 5}}, nil)
			if strategy != tc.strategy {
				t.Fatal(strategy)
			}
			for _, want := range tc.want {
				if !contains(refs, want) {
					t.Fatalf("missing %s in %v", want, refs)
				}
			}
			if strategy == "row_headers" && reason != "no_field_columns" {
				t.Fatal(reason)
			}
			if tc.name == "ambiguous" && len(fields) != 2 {
				t.Fatal(fields)
			}
		})
	}
}
