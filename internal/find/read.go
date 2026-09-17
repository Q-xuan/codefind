package find

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ReadRequest struct {
	Root     string        `json:"-"`
	File     string        `json:"file"`
	Sheet    string        `json:"sheet"`
	Range    string        `json:"range,omitempty"`
	Anchor   string        `json:"anchor,omitempty"`
	Fields   []string      `json:"fields,omitempty"`
	Strategy string        `json:"strategy"`
	MaxCells int           `json:"max_cells"`
	MaxChars int           `json:"max_chars"`
	Timeout  time.Duration `json:"-"`
}
type ReadCell struct {
	Cell          string           `json:"cell"`
	Type          string           `json:"type,omitempty"`
	Value         *string          `json:"value,omitempty"`
	Formula       *FormulaEvidence `json:"formula,omitempty"`
	CachedValue   *string          `json:"cached_value,omitempty"`
	Comment       *string          `json:"comment,omitempty"`
	TextTruncated bool             `json:"text_truncated,omitempty"`
}
type ReadResult struct {
	Engine          string       `json:"engine"`
	Version         string       `json:"version"`
	SchemaVersion   string       `json:"schema_version"`
	Status          string       `json:"status"`
	Query           ReadRequest  `json:"query"`
	Strategy        string       `json:"strategy"`
	FallbackReason  string       `json:"fallback_reason,omitempty"`
	FieldCandidates []string     `json:"field_candidates"`
	Cells           []ReadCell   `json:"cells"`
	Merges          []string     `json:"merged_ranges"`
	Coverage        ReadCoverage `json:"coverage"`
	Unknowns        []string     `json:"unknowns"`
	ExternalWrites  int          `json:"external_writes"`
}
type ReadCoverage struct {
	Ranges        []string `json:"ranges"`
	ParseComplete bool     `json:"parse_complete"`
	Truncated     bool     `json:"truncated"`
	CellsScanned  int      `json:"cells_scanned"`
	CellsSelected int      `json:"cells_selected"`
	ElapsedMS     int64    `json:"elapsed_ms"`
	TimeoutMS     int64    `json:"timeout_ms"`
}
type rect struct{ r1, c1, r2, c2 int }

func cellPosition(s string) (int, int, error) {
	if !validCell(s) {
		return 0, 0, fmt.Errorf("invalid cell %q", s)
	}
	i, c := 0, 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		c = c*26 + int(s[i]-'A') + 1
		i++
	}
	r, _ := strconv.Atoi(s[i:])
	return r, c, nil
}
func cellAddress(r, c int) string {
	s := ""
	for c > 0 {
		c--
		s = string(rune('A'+c%26)) + s
		c /= 26
	}
	return s + strconv.Itoa(r)
}
func parseRect(s string) (rect, error) {
	parts := strings.Split(s, ":")
	if len(parts) > 2 {
		return rect{}, errors.New("invalid range")
	}
	r, c, e := cellPosition(parts[0])
	if e != nil {
		return rect{}, e
	}
	out := rect{r, c, r, c}
	if len(parts) == 2 {
		out.r2, out.c2, e = cellPosition(parts[1])
		if e != nil {
			return rect{}, e
		}
	}
	if out.r2 < r || out.c2 < c {
		return rect{}, errors.New("reversed range")
	}
	return out, nil
}
func (b rect) contains(r, c int) bool { return r >= b.r1 && r <= b.r2 && c >= b.c1 && c <= b.c2 }
func (b rect) label() string          { return cellAddress(b.r1, b.c1) + ":" + cellAddress(b.r2, b.c2) }

func validateRead(req ReadRequest) (ReadRequest, string, []rect, error) {
	invalid := func(e error) (ReadRequest, string, []rect, error) {
		return req, "", nil, fmt.Errorf("%w: %w", ErrInvalidRequest, e)
	}
	n, e := normalizeRequest(Request{Root: req.Root, Terms: []string{"read"}, Timeout: req.Timeout})
	if e != nil {
		return invalid(e)
	}
	req.Timeout = n.Timeout
	if req.MaxCells == 0 {
		req.MaxCells = 96
	}
	if req.MaxChars == 0 {
		req.MaxChars = 24000
	}
	if req.MaxCells < 1 || req.MaxCells > 4096 || req.MaxChars < 1 || req.MaxChars > 200000 {
		return invalid(errors.New("max-cells 必须在 1..4096，max-chars 在 1..200000"))
	}
	if req.Sheet == "" || req.File == "" || filepath.IsAbs(req.File) || filepath.VolumeName(req.File) != "" {
		return invalid(errors.New("需要 sheet 和 root-relative file"))
	}
	cleanFile := filepath.Clean(req.File)
	if cleanFile == ".." || strings.HasPrefix(cleanFile, ".."+string(filepath.Separator)) {
		return invalid(errors.New("file escapes root"))
	}
	physical, e := filepath.EvalSymlinks(filepath.Join(n.root, req.File))
	if e != nil {
		return invalid(e)
	}
	if !inside(n.root, physical) {
		return invalid(errors.New("file escapes root"))
	}
	info, e := os.Stat(physical)
	if e != nil || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(physical), ".xlsx") {
		return invalid(errors.New("file 必须是 XLSX 普通文件"))
	}
	rel, _ := filepath.Rel(n.root, filepath.Join(n.root, req.File))
	req.File = filepath.ToSlash(rel)
	if (req.Range == "") == (req.Anchor == "") {
		return invalid(errors.New("range 与 anchor 必须且只能提供一个"))
	}
	req.Fields = cleanList(req.Fields)
	if req.Range != "" {
		if req.Strategy != "" && req.Strategy != "range" || len(req.Fields) > 0 {
			return invalid(errors.New("range 不接受字段或其他策略"))
		}
		req.Strategy = "range"
		b, e := parseRect(req.Range)
		if e != nil {
			return invalid(e)
		}
		if (b.r2-b.r1+1)*(b.c2-b.c1+1) > 4096 {
			return invalid(errors.New("range 最多 4096 个格子"))
		}
		return req, physical, []rect{b}, nil
	}
	if req.Strategy == "" {
		req.Strategy = "adaptive"
	}
	if req.Strategy != "adaptive" && req.Strategy != "structure" && req.Strategy != "row_headers" && req.Strategy != "window" {
		return invalid(errors.New("strategy 必须为 adaptive、structure、row_headers 或 window"))
	}
	if (req.Strategy == "adaptive" || req.Strategy == "structure") && len(req.Fields) == 0 {
		return invalid(errors.New("结构策略需要至少一个 --field"))
	}
	r, c, e := cellPosition(req.Anchor)
	if e != nil {
		return invalid(e)
	}
	if req.Strategy == "window" {
		return req, physical, []rect{{max(1, r-2), max(1, c-4), min(1048576, r+2), min(16384, c+4)}}, nil
	}
	if c > 64 {
		return invalid(errors.New("结构/表头策略只覆盖前 64 列，请使用 range 或 window"))
	}
	boxes := []rect{{max(1, r-32), 1, r, 64}}
	if req.Strategy == "adaptive" {
		boxes[0].r2 = min(1048576, r+16)
	}
	if req.Strategy == "row_headers" {
		boxes = []rect{{r, 1, r, 64}}
	}
	if boxes[0].r1 > 1 {
		boxes = append(boxes, rect{1, 1, min(12, boxes[0].r1-1), 64})
	}
	return req, physical, boxes, nil
}

func ReadWorkbook(ctx context.Context, request ReadRequest) (ReadResult, error) {
	started := time.Now()
	req, file, boxes, e := validateRead(request)
	if e != nil {
		return ReadResult{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	out := ReadResult{Engine: "codefind", Version: Version, SchemaVersion: "codefind-read-v1", Status: "read_complete", Query: req, Strategy: req.Strategy, Cells: []ReadCell{}, Merges: []string{}, FieldCandidates: []string{}, Unknowns: []string{"只返回原始证据，不确认字段含义；公式未重算，缓存可能过期，不包含图片、线程评论或跨表说明。"}}
	out.Coverage.TimeoutMS = req.Timeout.Milliseconds()
	for _, b := range boxes {
		out.Coverage.Ranges = append(out.Coverage.Ranges, b.label())
	}
	selectedRange := func(ref string) bool {
		r, c, err := cellPosition(ref)
		if err != nil {
			return false
		}
		for _, b := range boxes {
			if b.contains(r, c) {
				return true
			}
		}
		return false
	}
	cells := map[string]ReadCell{}
	merges := []rect{}
	tick := func() error {
		out.Coverage.CellsScanned++
		if out.Coverage.CellsScanned > 100000 {
			return errWorkbookBudget
		}
		return ctx.Err()
	}
	e = walkWorkbook(ctx, file, nil, workbookVisitor{sheet: req.Sheet,
		wantRow: func(r int) bool {
			for _, b := range boxes {
				if r >= b.r1 && r <= b.r2 {
					return true
				}
			}
			return false
		},
		wantCell: selectedRange,
		cell: func(_ string, c xlsxCell) error {
			if e := tick(); e != nil {
				return e
			}
			if selectedRange(c.Ref) {
				v := ReadCell{Cell: c.Ref, Type: c.Type, Value: c.Value, Formula: c.Formula}
				if c.Formula != nil {
					v.CachedValue = c.Value
					v.Value = nil
				}
				cells[c.Ref] = v
			}
			return nil
		},
		comment: func(_, ref, text string) error {
			if !selectedRange(ref) {
				return nil
			}
			if e := tick(); e != nil {
				return e
			}
			v := cells[ref]
			v.Cell = ref
			v.Comment = &text
			cells[ref] = v
			return nil
		},
		merge: func(_, ref string) error {
			b, e := parseRect(ref)
			if e != nil {
				return e
			}
			if len(merges) >= 4096 {
				return errWorkbookBudget
			}
			merges = append(merges, b)
			return ctx.Err()
		},
	})
	if e != nil {
		if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, errWorkbookBudget) {
			out.Coverage.Truncated = true
			out.Unknowns = append(out.Unknowns, "工作表读取未完成：达到时间、扫描量或解压预算。")
		} else {
			return ReadResult{}, e
		}
	} else {
		out.Coverage.ParseComplete = true
	}
	refs, fields, strategy, reason := selectReadCellsContext(ctx, req, cells, merges, boxes)
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ReadResult{}, ctx.Err()
		}
		out.Coverage.Truncated = true
	}
	out.Strategy = strategy
	out.FallbackReason = reason
	out.FieldCandidates = fields
	out.Coverage.CellsSelected = len(refs)
	if len(fields) > 1 {
		out.Unknowns = append(out.Unknowns, "存在多个候选字段列；保留全部候选，不自动判断值区、备注区或业务关联。")
	}
	if strategy != "range" {
		out.Unknowns = append(out.Unknowns, "策略使用有限坐标范围；未找到说明时应由调用方继续扩大范围或跨表查询。")
	}
	remaining := req.MaxChars
	for _, ref := range refs {
		if ctx.Err() != nil {
			out.Coverage.Truncated = true
			break
		}
		if len(out.Cells) >= req.MaxCells || remaining <= 0 {
			out.Coverage.Truncated = true
			break
		}
		v := cells[ref]
		clip := func(p *string) *string {
			if p == nil {
				return nil
			}
			s := []rune(*p)
			if len(s) > remaining {
				s = s[:remaining]
				v.TextTruncated = true
				out.Coverage.Truncated = true
			}
			remaining -= len(s)
			t := string(s)
			return &t
		}
		v.Value = clip(v.Value)
		v.CachedValue = clip(v.CachedValue)
		v.Comment = clip(v.Comment)
		if v.Formula != nil {
			f := *v.Formula
			f.Expression = *clip(&f.Expression)
			f.Type = *clip(&f.Type)
			f.SharedIndex = *clip(&f.SharedIndex)
			f.Range = *clip(&f.Range)
			v.Formula = &f
		}
		v.Type = *clip(&v.Type)
		out.Cells = append(out.Cells, v)
	}
	for _, b := range merges {
		if ctx.Err() != nil {
			out.Coverage.Truncated = true
			break
		}
		for _, box := range boxes {
			if b.r1 <= box.r2 && b.r2 >= box.r1 && b.c1 <= box.c2 && b.c2 >= box.c1 {
				out.Merges = append(out.Merges, b.label())
				if !selectedRange(cellAddress(b.r1, b.c1)) {
					out.Unknowns = append(out.Unknowns, "合并区域左上角位于读取范围外，请显式扩大范围后回读其存储值。")
				}
				break
			}
		}
	}
	if out.Coverage.Truncated {
		out.Status = StatusBudgetExceeded
		out.Unknowns = append(out.Unknowns, "输出或读取不完整，请查看 coverage 和 text_truncated；不要把缺失解释为不存在。")
	}
	out.Coverage.ElapsedMS = time.Since(started).Milliseconds()
	return out, nil
}

func selectReadCells(req ReadRequest, cells map[string]ReadCell, merges []rect, boxes []rect) ([]string, []string, string, string) {
	return selectReadCellsContext(context.Background(), req, cells, merges, boxes)
}

func selectReadCellsContext(ctx context.Context, req ReadRequest, cells map[string]ReadCell, merges []rect, boxes []rect) ([]string, []string, string, string) {
	refs := []string{}
	seen := map[string]bool{}
	fields := []string{}
	var add func(int, int)
	add = func(r, c int) {
		if ctx.Err() != nil {
			return
		}
		ref := cellAddress(r, c)
		v, ok := cells[ref]
		if ok && !seen[ref] && (v.Value != nil || v.Formula != nil || v.Comment != nil) {
			seen[ref] = true
			refs = append(refs, ref)
		}
		for _, b := range merges {
			if b.contains(r, c) && (r != b.r1 || c != b.c1) {
				key := cellAddress(b.r1, b.c1)
				v, ok := cells[key]
				if ok && !seen[key] && (v.Value != nil || v.Formula != nil || v.Comment != nil) {
					seen[key] = true
					refs = append(refs, key)
				}
			}
		}
	}
	if req.Strategy == "range" || req.Strategy == "window" {
		b := boxes[0]
		for r := b.r1; r <= b.r2; r++ {
			for c := b.c1; c <= b.c2; c++ {
				add(r, c)
			}
		}
		return refs, fields, req.Strategy, ""
	}
	r, c, _ := cellPosition(req.Anchor)
	strategy := req.Strategy
	reason := ""
	if strategy == "adaptive" || strategy == "structure" {
		anchor := cells[req.Anchor]
		keys := []int{}
		if anchor.Value != nil {
			for col := 1; col <= 64; col++ {
				v := cells[cellAddress(r, col)]
				if v.Value != nil && *v.Value == *anchor.Value {
					keys = append(keys, col)
				}
			}
		}
		add(r, c)
		for i, key := range keys {
			add(r, key)
			end := 65
			if i+1 < len(keys) {
				end = keys[i+1]
			}
			for col := key + 1; col < end; col++ {
				found := false
				for row := r - 1; row >= max(1, r-32); row-- {
					v := cells[cellAddress(row, col)]
					if v.Value != nil {
						for _, alias := range req.Fields {
							if strings.EqualFold(strings.TrimSpace(*v.Value), alias) {
								add(row, col)
								found = true
								break
							}
						}
					}
				}
				if found {
					fields = append(fields, cellAddress(r, col))
					add(r, col)
				}
			}
		}
		if len(fields) > 0 || strategy == "structure" {
			return refs, fields, "structure", ""
		}
		// A title may precede the field header. Return bounded candidate
		// columns plus nearby row labels, without inferring their meaning.
		for col := 1; col <= 64; col++ {
			header := 0
			for row := r + 1; row <= min(1048576, r+8) && header == 0; row++ {
				v := cells[cellAddress(row, col)]
				if v.Value != nil {
					for _, alias := range req.Fields {
						if strings.EqualFold(strings.TrimSpace(*v.Value), alias) {
							header = row
							break
						}
					}
				}
			}
			if header == 0 {
				continue
			}
			fields = append(fields, cellAddress(header, col))
			for row := header; row <= min(1048576, header+8); row++ {
				add(row, col)
				for label := c; label <= min(64, c+2); label++ {
					add(row, label)
				}
			}
		}
		if len(fields) > 0 {
			return refs, fields, "below_headers", "no_field_columns_above"
		}
		strategy = "row_headers"
		reason = "no_field_columns"
		refs = []string{}
		seen = map[string]bool{}
	}
	for col := 1; col <= 64; col++ {
		add(r, col)
	}
	for row := 1; row <= min(12, r-1); row++ {
		for col := 1; col <= 64; col++ {
			add(row, col)
		}
	}
	return refs, fields, strategy, reason
}
