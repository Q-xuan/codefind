package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Q-xuan/codefind/internal/find"
)

// This test uses a real executable and chooses readback coordinates only from
// its search response. The workbook is synthetic and contains no private data.
func TestCLIEndToEnd(t *testing.T) {
	binary := os.Getenv("CODEFIND_TEST_BIN")
	if binary == "" {
		binary = filepath.Join(t.TempDir(), "codefind")
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if out, e := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); e != nil {
			t.Fatalf("build: %v %s", e, out)
		}
	}
	invoke := func(args []string, want int) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		e := cmd.Run()
		code := 0
		if e != nil {
			if ex, ok := e.(*exec.ExitError); ok {
				code = ex.ExitCode()
			} else {
				t.Fatal(e)
			}
		}
		if code != want {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		return stdout.Bytes()
	}
	if got := string(bytes.TrimSpace(invoke([]string{"--version"}, 0))); got != find.Version {
		t.Fatal(got)
	}
	help := string(invoke([]string{"--help-xlsx"}, 0))
	for _, want := range []string{"--range", "--field", "unknown", "10s", "next_path"} {
		if !bytes.Contains([]byte(help), []byte(want)) {
			t.Fatalf("help-xlsx missing %s: %s", want, help)
		}
	}
	root := t.TempDir()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:relationships"><sheets><sheet name="rules" r:id="r1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="r1" Type="x/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row><c r="C11" t="inlineStr"><is><t>FeatureToken</t></is></c><c r="G12" t="inlineStr"><is><t>Parameter2</t></is></c><c r="G16"><v>42</v></c><c r="G17" t="inlineStr"><is><t>Maximum characters</t></is></c></row></sheetData></worksheet>`,
	}
	for name, xml := range parts {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(xml)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	original := buf.Bytes()
	file := filepath.Join(root, "example.xlsx")
	if e := os.WriteFile(file, original, 0600); e != nil {
		t.Fatal(e)
	}
	var search find.Result
	if e := json.Unmarshal(invoke([]string{"--root", root, "--format", "xlsx", "--path", "example.xlsx", "--term", "FeatureToken", "--timeout", "10s"}, 0), &search); e != nil {
		t.Fatal(e)
	}
	if len(search.Anchors) != 1 || search.WorkbookCoverage == nil || !search.WorkbookCoverage.DiscoveryComplete {
		t.Fatalf("search=%+v", search)
	}
	a := search.Anchors[0]
	if a.Workbook == nil {
		t.Fatal("missing location")
	}
	var read find.ReadResult
	args := []string{"read", "--root", root, "--file", a.Path, "--sheet", a.Workbook.Sheet, "--anchor", a.Workbook.Cell, "--field", "Parameter2"}
	if e := json.Unmarshal(invoke(args, 0), &read); e != nil {
		t.Fatal(e)
	}
	if read.Strategy != "below_headers" || read.Coverage.Truncated {
		t.Fatalf("read=%+v", read)
	}
	found := false
	for _, c := range read.Cells {
		if c.Cell == "G17" && c.Value != nil && *c.Value == "Maximum characters" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing explanation")
	}
	var clipped find.ReadResult
	if e := json.Unmarshal(invoke(append(args, "--max-cells", "1"), 0), &clipped); e != nil {
		t.Fatal(e)
	}
	if !clipped.Coverage.Truncated || clipped.Status != find.StatusBudgetExceeded {
		t.Fatal(clipped)
	}
	for _, args := range [][]string{{"--timeout", "bad"}, {"read", "--root", root, "--file", "../outside.xlsx", "--sheet", "rules", "--range", "A1"}} {
		var failure map[string]any
		if e := json.Unmarshal(invoke(args, 2), &failure); e != nil || failure["status"] != "invalid_request" {
			t.Fatalf("failure=%v error=%v", failure, e)
		}
	}
	data, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(original) != sha256.Sum256(data) {
		t.Fatal("workbook changed")
	}
}
