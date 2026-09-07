package find

import (
	"fmt"
	"testing"
)

func TestWorkbookProjectionDiversity(t *testing.T) {
	rows := []Anchor{}
	add := func(book, sheet string, n int) {
		for i := 0; i < n; i++ {
			rows = append(rows, Anchor{Path: book, Text: fmt.Sprint(i), Workbook: &WorkbookLocation{Sheet: sheet, Cell: fmt.Sprintf("A%d", i+1)}})
		}
	}
	add("a.xlsx", "noisy", 30)
	add("b.xlsx", "notes", 20)
	add("b.xlsx", "common", 2)
	add("c.xlsx", "items", 1)
	out := projectWorkbookAnchors(rows, 6)
	want := []string{"a.xlsx/noisy", "b.xlsx/notes", "c.xlsx/items", "a.xlsx/noisy", "b.xlsx/common", "a.xlsx/noisy"}
	for i, a := range out {
		if got := a.Path + "/" + a.Workbook.Sheet; got != want[i] {
			t.Fatalf("%d: %s want %s", i, got, want[i])
		}
	}
	if len(out) != 6 || out[3].Text != "1" {
		t.Fatalf("out=%+v", out)
	}
}

func TestWorkbookProjectionLimitsAndIdentity(t *testing.T) {
	rows := []Anchor{{Path: "a", Workbook: &WorkbookLocation{Sheet: "same", Cell: "A1", Source: "cell"}}, {Path: "b", Workbook: &WorkbookLocation{Sheet: "same", Cell: "A1"}}, {Path: "a", Workbook: &WorkbookLocation{Sheet: "same", Cell: "A1", Source: "comment"}}}
	for _, limit := range []int{0, 1, 2, 10} {
		out := projectWorkbookAnchors(rows, limit)
		if len(out) != min(limit, len(rows)) {
			t.Fatal(len(out))
		}
	}
	out := projectWorkbookAnchors(rows, 10)
	if out[2].Workbook.Source != "comment" {
		t.Fatal("comment lost")
	}
	if len(projectWorkbookAnchors(nil, 10)) != 0 {
		t.Fatal("nonempty result")
	}
}
