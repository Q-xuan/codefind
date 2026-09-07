package find

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workbookFixture(t *testing.T, overrides map[string]string) string {
	t.Helper()
	parts := map[string]string{
		"xl/workbook.xml":                     `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="common" r:id="r1"/><sheet name="items" r:id="r2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels":          `<Relationships><Relationship Id="r1" Type="x/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="r2" Type="x/worksheet" Target="/xl/worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml":                `<sst><si><r><t>P</t></r><r><t>vp</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml":            `<worksheet><sheetData><row r="2"><c r="B2" t="s"><v>0</v></c><c r="C2"><f>1+1</f><v>2</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml":            `<worksheet><sheetData><row r="2"><c r="B2" t="inlineStr"><is><t>挑战券</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/_rels/sheet1.xml.rels": `<Relationships><Relationship Id="note" Type="x/comments" Target="../comments1.xml"/></Relationships>`,
		"xl/comments1.xml":                    `<comments><commentList><comment ref="D2"><text><r><t>Pvp 挑战</t></r><r><t>次数说明</t></r></text></comment></commentList></comments>`,
	}
	for k, v := range overrides {
		parts[k] = v
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for k, v := range parts {
		w, e := z.Create(k)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(v)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "design.xlsx"), buf.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	return root
}

func TestXLSXLocationsAndComments(t *testing.T) {
	root := workbookFixture(t, nil)
	t.Setenv("PATH", "") // Native mode must not require rg or Excel.
	result, err := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp", "挑战券"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCandidatesFound || len(result.Anchors) != 3 || result.Metrics.RGCalls != 0 {
		t.Fatalf("result=%+v", result)
	}
	want := map[string]string{"common/B2/cell": "Pvp", "common/D2/comment": "Pvp 挑战次数说明", "items/B2/cell": "挑战券"}
	for _, a := range result.Anchors {
		if a.Workbook == nil || a.Line != 0 {
			t.Fatalf("anchor=%+v", a)
		}
		k := a.Workbook.Sheet + "/" + a.Workbook.Cell + "/" + a.Workbook.Source
		if want[k] != a.Text {
			t.Fatalf("%s: %s", k, a.Text)
		}
		delete(want, k)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
}

func TestXLSXLimitsAndOverlappingPaths(t *testing.T) {
	root := workbookFixture(t, nil)
	for _, tc := range []struct {
		matches, anchors int
		status           string
	}{{1, 1, StatusBudgetExceeded}, {10, 1, StatusCandidatesFound}, {10, 10, StatusCandidatesFound}} {
		r, e := Find(context.Background(), Request{Root: root, Paths: []string{".", "./"}, Format: "xlsx", Terms: []string{"Pvp", "挑战券"}, MaxMatches: tc.matches, MaxAnchors: tc.anchors})
		if e != nil || r.Status != tc.status || len(r.Anchors) > tc.anchors {
			t.Fatalf("r=%+v e=%v", r, e)
		}
		if tc.matches == 10 && r.Metrics.RawMatches != 3 {
			t.Fatal(r.Metrics)
		}
	}
}

func TestXLSXRejectsMalformedParts(t *testing.T) {
	for _, parts := range []map[string]string{
		{"xl/sharedStrings.xml": "<sst>"},
		{"xl/worksheets/sheet1.xml": `<worksheet><c r="B2" t="s"><v>99</v></c></worksheet>`},
		{"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="r1" Type="x/worksheet" Target="../../outside.xml"/></Relationships>`},
	} {
		root := workbookFixture(t, parts)
		_, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
		if e == nil {
			t.Fatal("expected error")
		}
	}
}

func TestXLSXCancellationAndPartBudget(t *testing.T) {
	root := workbookFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := Find(ctx, Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, _ := z.Create("large.xml")
	_, _ = w.Write([]byte(strings.Repeat("x", 128)))
	_ = z.Close()
	zr, e := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if e != nil {
		t.Fatal(e)
	}
	a := xlsxArchive{files: map[string]*zip.File{"large.xml": zr.File[0]}, remaining: 64, ctx: context.Background()}
	if e := a.decode("large.xml", nil); !errors.Is(e, errWorkbookBudget) {
		t.Fatal(e)
	}
}

func TestXLSXFormatValidation(t *testing.T) {
	for _, req := range []Request{{Format: "xls"}, {Format: "xlsx", Encoding: "gbk"}} {
		_, e := Find(context.Background(), req)
		if !errors.Is(e, ErrInvalidRequest) {
			t.Fatal(e)
		}
	}
}

func TestXLSXSkipsLockAndHiddenDirectories(t *testing.T) {
	root := workbookFixture(t, nil)
	for _, name := range []string{"~$locked.xlsx", ".git/private.xlsx", "vendor/private.xlsx", "node_modules/private.xlsx"} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("not a zip"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	r, e := Find(context.Background(), Request{Root: root, Format: "xlsx", Terms: []string{"Pvp"}})
	if e != nil || r.Metrics.XLSXFilesScanned != 1 {
		t.Fatalf("r=%+v err=%v", r, e)
	}
}

func TestXLSXCellAddresses(t *testing.T) {
	for _, s := range []string{"A1", "XFD1048576", "AA20"} {
		if !validCell(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"XFE1", "A0", "A01", "A-1", "A+1", "A1048577", "1", ""} {
		if validCell(s) {
			t.Fatal(s)
		}
	}
}
