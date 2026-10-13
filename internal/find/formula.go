package find

import (
	"strconv"
	"strings"
)

type sharedMaster struct {
	cell string
	expr string
}

// shiftFormula rewrites A1-style references by a row/column delta.
// It returns ok=false for constructs it will not rewrite, and never evaluates the formula.
func shiftFormula(expr string, dr, dc int) (string, bool) {
	if hasR1C1(expr) {
		return "", false
	}
	var b strings.Builder
	prevIdent := false
	for i := 0; i < len(expr); {
		if expr[i] == '"' {
			n, ok := skipQuoted(expr[i:])
			if !ok {
				return "", false
			}
			b.WriteString(expr[i : i+n])
			i += n
			prevIdent = false
			continue
		}
		if expr[i] == '[' {
			return "", false
		}
		if !prevIdent && columnOrRowSpan(expr[i:]) > 0 {
			return "", false
		}
		if !prevIdent {
			if n, repl, matched, ok := consumeRef(expr[i:], dr, dc); matched {
				if !ok {
					return "", false
				}
				b.WriteString(repl)
				i += n
				prevIdent = false
				continue
			}
		}
		b.WriteByte(expr[i])
		prevIdent = isNameChar(expr[i])
		i++
	}
	return b.String(), true
}

func skipQuoted(s string) (int, bool) {
	if s == "" || s[0] != '"' {
		return 0, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '"' {
			i++
			continue
		}
		return i + 1, true
	}
	return 0, false
}

func hasR1C1(expr string) bool {
	s := strings.ToUpper(maskStrings(expr))
	for i := 0; i < len(s); i++ {
		if s[i] != 'R' {
			continue
		}
		if i > 0 && isNameChar(s[i-1]) {
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '[' {
			return true
		}
		if j >= len(s) || !isDigit(s[j]) {
			continue
		}
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j < len(s) && s[j] == 'C' && j+1 < len(s) && (isDigit(s[j+1]) || s[j+1] == '[') {
			return true
		}
	}
	return false
}

func maskStrings(expr string) string {
	var b strings.Builder
	for i := 0; i < len(expr); {
		if expr[i] == '"' {
			n, ok := skipQuoted(expr[i:])
			if !ok {
				b.WriteByte(' ')
				i++
				continue
			}
			for k := 0; k < n; k++ {
				b.WriteByte(' ')
			}
			i += n
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

func consumeRef(s string, dr, dc int) (n int, repl string, matched bool, ok bool) {
	if s == "" {
		return 0, "", false, true
	}
	sheet := ""
	rest := s
	if s[0] == '\'' {
		j := 1
		closed := false
		for j < len(s) {
			if s[j] == '\'' {
				if j+1 < len(s) && s[j+1] == '\'' {
					j += 2
					continue
				}
				j++
				closed = true
				break
			}
			j++
		}
		if !closed || j >= len(s) || s[j] != '!' {
			return 0, "", true, false
		}
		body := strings.ReplaceAll(s[1:j-1], "''", "")
		if strings.Contains(body, ":") {
			return j + 1, "", true, false
		}
		sheet = s[:j+1]
		rest = s[j+1:]
	} else if isNameStart(s[0]) {
		j := 0
		for j < len(s) && isNameChar(s[j]) {
			j++
		}
		if j < len(s) && s[j] == '!' {
			sheet = s[:j+1]
			rest = s[j+1:]
		} else if j < len(s) && s[j] == ':' {
			if _, _, _, _, _, cok := parseCellToken(s); cok {
				rest = s
			} else if n := columnOrRowSpan(s); n > 0 {
				return n, "", true, false
			} else {
				k := j + 1
				for k < len(s) && isNameChar(s[k]) {
					k++
				}
				if k < len(s) && s[k] == '!' {
					return k + 1, "", true, false
				}
				return 0, "", false, true
			}
		} else {
			rest = s
		}
	} else if s[0] == '$' {
		rest = s
	} else {
		return 0, "", false, true
	}

	cn, cell, cok := shiftCell(rest, dr, dc)
	if !cok {
		if cn > 0 || sheet != "" {
			if n := columnOrRowSpan(rest); n > 0 {
				return len(sheet) + n, "", true, false
			}
			return len(sheet) + max(cn, 0), "", true, false
		}
		if n := columnOrRowSpan(rest); n > 0 {
			return n, "", true, false
		}
		return 0, "", false, true
	}
	total := len(sheet) + cn
	out := sheet + cell
	after := rest[cn:]
	if strings.HasPrefix(after, ":") {
		n2, repl2, matched2, ok2 := consumeRef(after[1:], dr, dc)
		if !matched2 || !ok2 {
			return total, "", true, false
		}
		total = len(sheet) + cn + 1 + n2
		out = sheet + cell + ":" + repl2
	}
	if sheet == "" && total < len(s) && s[total] == '(' {
		return 0, "", false, true
	}
	return total, out, true, true
}

func shiftCell(s string, dr, dc int) (int, string, bool) {
	n, colAbs, col, rowAbs, row, ok := parseCellToken(s)
	if !ok {
		return 0, "", false
	}
	if !colAbs {
		col += dc
	}
	if !rowAbs {
		row += dr
	}
	formatted, fok := formatRef(colAbs, col, rowAbs, row)
	if !fok {
		return n, "", false
	}
	return n, formatted, true
}

func parseCellToken(s string) (n int, colAbs bool, col int, rowAbs bool, row int, ok bool) {
	i := 0
	if i < len(s) && s[i] == '$' {
		colAbs = true
		i++
	}
	start := i
	for i < len(s) && isLetter(s[i]) {
		i++
	}
	letters := i - start
	if letters < 1 || letters > 3 {
		return 0, false, 0, false, 0, false
	}
	if i < len(s) && s[i] == '$' {
		rowAbs = true
		i++
	}
	rowStart := i
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == rowStart {
		return 0, false, 0, false, 0, false
	}
	row, err := strconv.Atoi(s[rowStart:i])
	if err != nil || row < 1 || row > 1048576 {
		return 0, false, 0, false, 0, false
	}
	col = 0
	for _, ch := range strings.ToUpper(s[start:rowStart]) {
		if ch == '$' {
			continue
		}
		col = col*26 + int(ch-'A') + 1
	}
	if col < 1 || col > 16384 {
		return 0, false, 0, false, 0, false
	}
	return i, colAbs, col, rowAbs, row, true
}

func formatRef(colAbs bool, col int, rowAbs bool, row int) (string, bool) {
	if col < 1 || col > 16384 || row < 1 || row > 1048576 {
		return "", false
	}
	var b strings.Builder
	if colAbs {
		b.WriteByte('$')
	}
	b.WriteString(columnName(col))
	if rowAbs {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(row))
	return b.String(), true
}

func columnName(c int) string {
	s := ""
	for c > 0 {
		c--
		s = string(rune('A'+c%26)) + s
		c /= 26
	}
	return s
}

func columnOrRowSpan(s string) int {
	i := 0
	if i < len(s) && s[i] == '$' {
		i++
	}
	if i < len(s) && isLetter(s[i]) {
		start := i
		for i < len(s) && isLetter(s[i]) {
			i++
		}
		if i-start > 3 || i >= len(s) || s[i] != ':' {
			return 0
		}
		i++
		if i < len(s) && s[i] == '$' {
			i++
		}
		if i >= len(s) || !isLetter(s[i]) {
			return 0
		}
		n := 0
		for i < len(s) && isLetter(s[i]) {
			i++
			n++
		}
		if n < 1 || n > 3 {
			return 0
		}
		return i
	}
	if i < len(s) && isDigit(s[i]) {
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i >= len(s) || s[i] != ':' {
			return 0
		}
		i++
		if i < len(s) && s[i] == '$' {
			i++
		}
		if i >= len(s) || !isDigit(s[i]) {
			return 0
		}
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		return i
	}
	return 0
}

func isLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isNameStart(b byte) bool { return isLetter(b) || b == '_' }

func isNameChar(b byte) bool {
	return isLetter(b) || isDigit(b) || b == '_' || b == '.'
}

func projectSharedFormulas(cells map[string]ReadCell, masters map[string]sharedMaster) (mechanical, unknown bool) {
	for ref, cell := range cells {
		if cell.Formula == nil || cell.Formula.Type != "shared" || strings.TrimSpace(cell.Formula.Expression) != "" {
			continue
		}
		f := *cell.Formula
		m, found := masters[f.SharedIndex]
		if !found || strings.TrimSpace(m.expr) == "" {
			f.ResolveStatus = "unknown"
			unknown = true
			cell.Formula = &f
			cells[ref] = cell
			continue
		}
		f.SharedAnchor = m.cell
		f.AnchorExpression = m.expr
		ar, ac, e1 := cellPosition(m.cell)
		br, bc, e2 := cellPosition(ref)
		if e1 != nil || e2 != nil {
			f.ResolveStatus = "unknown"
			unknown = true
		} else if resolved, ok := shiftFormula(m.expr, br-ar, bc-ac); ok {
			f.ResolvedExpression = resolved
			f.ResolveStatus = "mechanical"
			mechanical = true
		} else {
			f.ResolveStatus = "unknown"
			unknown = true
		}
		cell.Formula = &f
		cells[ref] = cell
	}
	return mechanical, unknown
}
