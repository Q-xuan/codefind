package find

import "testing"

func TestShiftFormulaMechanicalRefs(t *testing.T) {
	for _, tc := range []struct {
		expr string
		dr   int
		dc   int
		want string
	}{
		{"B1+1", 1, 0, "B2+1"},
		{"$B$1+C1", 1, 1, "$B$1+D2"},
		{"Other!B1+'费用'!$C$1", 2, 0, "Other!B3+'费用'!$C$1"},
		{"SUM(A1:B2)", 0, 1, "SUM(B1:C2)"},
		{`LOG10(A1)`, 1, 0, `LOG10(A2)`},
	} {
		got, ok := shiftFormula(tc.expr, tc.dr, tc.dc)
		if !ok || got != tc.want {
			t.Fatalf("%s dr=%d dc=%d got=%q ok=%v", tc.expr, tc.dr, tc.dc, got, ok)
		}
	}
}

func TestShiftFormulaRejectsUnsupported(t *testing.T) {
	for _, expr := range []string{
		"SUM([1]Other!A1)",
		"SUM(A:A)",
		"SUM(1:2)",
		"Sheet1:Sheet2!A1",
		"R1C1",
		`INDIRECT(R1C1)`,
	} {
		if _, ok := shiftFormula(expr, 1, 0); ok {
			t.Fatalf("expected unknown for %s", expr)
		}
	}
	got, ok := shiftFormula(`INDIRECT("A1")+B1`, 1, 0)
	if !ok || got != `INDIRECT("A1")+B2` {
		t.Fatalf("string literal shifted: %q ok=%v", got, ok)
	}
}
