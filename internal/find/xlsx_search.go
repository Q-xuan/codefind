package find

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type WorkbookCoverage struct {
	DiscoveryComplete bool           `json:"discovery_complete"`
	ContentComplete   bool           `json:"content_complete"`
	NextPath          string         `json:"next_path,omitempty"`
	NextCommand       string         `json:"next_command,omitempty"`
	ResumeCursor      string         `json:"resume_cursor,omitempty"`
	Files             []WorkbookScan `json:"files"`
}
type WorkbookScan struct {
	Path           string      `json:"path"`
	Status         string      `json:"status"` // pending, complete, partial
	Reason         string      `json:"reason,omitempty"`
	MetadataStatus string      `json:"metadata_status"`
	Score          int         `json:"score"`
	Size           int64       `json:"size"`
	LimitKind      string      `json:"limit_kind,omitempty"`
	LimitBytes     int64       `json:"limit_bytes,omitempty"`
	ActualBytes    int64       `json:"actual_bytes,omitempty"`
	BudgetBytes    int64       `json:"budget_bytes,omitempty"`
	Entry          string      `json:"entry,omitempty"`
	Sheet          string      `json:"sheet,omitempty"`
	Detail         string      `json:"detail,omitempty"`
	Sheets         []SheetScan `json:"sheets,omitempty"`
}
type SheetScan struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	LimitKind   string `json:"limit_kind,omitempty"`
	LimitBytes  int64  `json:"limit_bytes,omitempty"`
	ActualBytes int64  `json:"actual_bytes,omitempty"`
	BudgetBytes int64  `json:"budget_bytes,omitempty"`
	Entry       string `json:"entry,omitempty"`
}
type workbookCandidate struct {
	physical, relative string
	size               int64
	score              int
	metadata           string
}

const (
	workbookMetaTimeout = 200 * time.Millisecond
	reasonDeferred      = "deferred"
)

// Names only affect ordering. A name mismatch never excludes a workbook.
func nameScore(name string, patterns []string) int {
	score := 0
	name = strings.ToLower(name)
	for _, p := range patterns {
		if strings.Contains(name, strings.ToLower(p)) {
			score++
		}
	}
	return score
}

func literalCount(text string, patterns []string) int {
	n := 0
	for _, p := range patterns {
		if p != "" {
			n += strings.Count(text, p)
		}
	}
	return n
}

func rankScore(nameHits, sheetHits, sstHits int) int {
	return nameHits*8 + sheetHits*4 + min(sstHits, 64)
}

// Mid-size design workbooks outrank tiny generic tables and very large books
// when cheap scores tie. Smaller-file-first is what picked the ~200KB decoy.
func sizeBand(size int64) int {
	switch {
	case size >= 512<<10 && size <= 6<<20:
		return 2
	case size > 6<<20:
		return 1
	default:
		return 0
	}
}

func workbookNameScore(ctx context.Context, filename string, patterns []string) (int, error) {
	sheet, sst, err := workbookHints(ctx, filename, patterns)
	return rankScore(0, sheet, sst), err
}

func workbookHints(ctx context.Context, filename string, patterns []string) (int, int, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return 0, 0, err
	}
	defer z.Close()
	a := xlsxArchive{ctx: ctx, remaining: 1 << 20, files: map[string]*zip.File{}}
	for _, f := range z.File {
		if f.Name == "xl/workbook.xml" || f.Name == "xl/sharedStrings.xml" {
			a.files[f.Name] = f
		}
	}
	sheet := 0
	sheetErr := a.decode("xl/workbook.xml", func(d *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local != "sheet" {
			return nil
		}
		for _, attr := range s.Attr {
			if attr.Name.Local == "name" {
				sheet = max(sheet, nameScore(attr.Value, patterns))
			}
		}
		return nil
	})
	sst := 0
	if a.files["xl/sharedStrings.xml"] != nil {
		sstErr := a.decode("xl/sharedStrings.xml", func(d *xml.Decoder, s xml.StartElement) error {
			if s.Name.Local != "si" {
				return nil
			}
			var v xlsxRichText
			if err := d.DecodeElement(&v, &s); err != nil {
				return err
			}
			sst += literalCount(v.value(), patterns)
			return nil
		})
		if sheetErr != nil {
			return sheet, sst, sheetErr
		}
		return sheet, sst, sstErr
	}
	return sheet, sst, sheetErr
}

func discoverWorkbooks(ctx context.Context, req normalizedRequest) ([]workbookCandidate, bool, error) {
	candidates := []workbookCandidate{}
	seen := map[string]bool{}
	add := func(filename string, size int64) error {
		physical, err := filepath.EvalSymlinks(filename)
		if err != nil {
			return err
		}
		if !inside(req.root, physical) {
			return errors.New("XLSX path escapes root")
		}
		if seen[physical] {
			return nil
		}
		relative, err := filepath.Rel(req.root, filename)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relative)
		if pathExcluded(rel, req.excludes) {
			return nil
		}
		seen[physical] = true
		if len(candidates) >= maxWorkbooks {
			return errWorkbookBudget
		}
		candidates = append(candidates, workbookCandidate{physical: physical, relative: rel, size: size, metadata: "pending"})
		return nil
	}
	scopes := req.includes
	if len(scopes) == 0 {
		scopes = req.Paths
	}
	for _, scope := range scopes {
		if pathExcluded(scope, req.excludes) {
			continue
		}
		base := filepath.Join(req.root, filepath.FromSlash(scope))
		info, err := os.Stat(base)
		if err != nil {
			return nil, false, err
		}
		if info.Mode().IsRegular() {
			if !strings.EqualFold(filepath.Ext(base), ".xlsx") || strings.HasPrefix(filepath.Base(base), "~$") {
				continue
			}
			if err := add(base, info.Size()); err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkbookBudget) {
					return candidates, false, nil
				}
				return nil, false, err
			}
			continue
		}
		err = filepath.WalkDir(base, func(filename string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if filename != base && strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				if filename != base {
					rel, relErr := filepath.Rel(req.root, filename)
					if relErr == nil && pathExcluded(filepath.ToSlash(rel), req.excludes) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(filename), ".xlsx") || strings.HasPrefix(d.Name(), "~$") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return add(filename, info.Size())
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkbookBudget) {
				return candidates, false, nil
			}
			return nil, false, err
		}
	}
	if !req.dirScope && len(req.includes) == 1 {
		rel := req.includes[0]
		base := filepath.Join(req.root, filepath.FromSlash(rel))
		info, err := os.Stat(base)
		if err == nil && info.Mode().IsRegular() {
			parent := filepath.Dir(base)
			if inside(req.root, parent) {
				err = filepath.WalkDir(parent, func(filename string, d fs.DirEntry, walkErr error) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					if walkErr != nil {
						return walkErr
					}
					if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
						if d.IsDir() && filename != parent {
							return filepath.SkipDir
						}
						return nil
					}
					if !strings.EqualFold(filepath.Ext(filename), ".xlsx") || strings.HasPrefix(d.Name(), "~$") {
						return nil
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					return add(filename, info.Size())
				})
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkbookBudget) {
						return candidates, false, nil
					}
					return nil, false, err
				}
			}
		}
	}
	return candidates, true, nil
}

type workbookScanner func(context.Context, string, []string, func(string, string, string, string) error) error

func findWorkbooks(ctx context.Context, req normalizedRequest, result Result, started time.Time) (Result, error) {
	return findWorkbooksWithScanner(ctx, req, result, started, scanWorkbook)
}
func findWorkbooksWithScanner(ctx context.Context, req normalizedRequest, result Result, started time.Time, scan workbookScanner) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	candidates, complete, err := discoverWorkbooks(ctx, req)
	if err != nil {
		return Result{}, err
	}
	patterns := append(append([]string{}, req.Symbols...), req.Terms...)
	// Metadata gets at most 20% of the request; each workbook gets at most 200ms
	// for sheet names plus a shared-string literal peek. Hints only rank files.
	metaCtx, metaCancel := context.WithTimeout(ctx, req.Timeout/5)
	for i := range candidates {
		c := &candidates[i]
		nameHits := nameScore(filepath.Base(c.relative), patterns)
		c.score = rankScore(nameHits, 0, 0)
		if metaCtx.Err() != nil || c.size > maxWorkbookBytes {
			c.metadata = "skipped"
			continue
		}
		one, cancel := context.WithTimeout(metaCtx, workbookMetaTimeout)
		sheet, sst, e := workbookHints(one, c.physical, patterns)
		cancel()
		c.score = rankScore(nameHits, sheet, sst)
		if e == nil {
			c.metadata = "complete"
		} else {
			c.metadata = "unavailable"
		}
	}
	metaCancel()
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if sizeBand(a.size) != sizeBand(b.size) {
			return sizeBand(a.size) > sizeBand(b.size)
		}
		return a.relative < b.relative
	})
	coverage := &WorkbookCoverage{DiscoveryComplete: complete, Files: []WorkbookScan{}}
	for _, c := range candidates {
		coverage.Files = append(coverage.Files, WorkbookScan{Path: c.relative, Status: "pending", Reason: "total_timeout", MetadataStatus: c.metadata, Score: c.score, Size: c.size})
	}
	result.WorkbookCoverage = coverage
	result.Unknowns = append(result.Unknowns, "XLSX 仅搜索单元格存储值和传统批注；不重算公式、不搜索图片或线程评论。")
	done := map[string]bool{}
	if req.Cursor != "" {
		paths, err := decodeResumeCursor(req.Cursor)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		for _, p := range paths {
			done[p] = true
		}
	}
	explicitBatch := req.MaxFiles > 0
	namedSingle := !req.dirScope && len(req.includes) == 1
	for i, c := range candidates {
		if done[c.relative] {
			coverage.Files[i].Status = "complete"
			coverage.Files[i].Reason = "cursor_skipped"
		}
	}
	scanIdx := []int{}
	if explicitBatch || req.Cursor != "" {
		limit := req.MaxFiles
		if limit == 0 {
			limit = 1
		}
		if namedSingle && !explicitBatch {
			for i, c := range candidates {
				if c.relative == req.includes[0] && !done[c.relative] {
					scanIdx = append(scanIdx, i)
				}
			}
		} else {
			for i, c := range candidates {
				if done[c.relative] {
					continue
				}
				scanIdx = append(scanIdx, i)
				if len(scanIdx) == limit {
					break
				}
			}
		}
		for i := range coverage.Files {
			if coverage.Files[i].Reason == "cursor_skipped" {
				continue
			}
			selected := false
			for _, j := range scanIdx {
				if j == i {
					selected = true
					break
				}
			}
			if !selected {
				coverage.Files[i].Status = "pending"
				coverage.Files[i].Reason = reasonDeferred
			}
		}
	} else if (req.dirScope || namedSingle) && len(candidates) > 1 {
		idx := 0
		if namedSingle {
			for i, c := range candidates {
				if c.relative == req.includes[0] {
					idx = i
					break
				}
			}
		}
		scanIdx = []int{idx}
		for i := range coverage.Files {
			if i != idx {
				coverage.Files[i].Status = "pending"
				coverage.Files[i].Reason = reasonDeferred
			}
		}
	} else {
		for i := range candidates {
			scanIdx = append(scanIdx, i)
		}
	}
	if coverageHasDeferred(coverage) {
		if explicitBatch || req.Cursor != "" {
			result.Unknowns = append(result.Unknowns, "本批只内容扫描指定数量的工作簿。未进入本批的文件仍是 deferred，不能当成全目录已扫描。续扫用 next_command，不会重复已完成文件。未扫描不等于没有该字段。")
		} else if len(scanIdx) <= 1 {
			result.Unknowns = append(result.Unknowns, "本次只内容扫描一簿。workbook_coverage 含 score/size；next_path 与 reason=deferred 是下一跳，请用 --path 点名。未扫描不等于没有该字段。")
		}
	}
	anchors := []Anchor{}
	matches := 0
	budget := !complete
	readFailed := false
	exact := req.Match == "exact"
	notedSheets := false
	// One selected workbook keeps the remaining request; a batch or named multi-file shares slices.
	slice := req.Timeout
	if len(scanIdx) > 1 {
		slice = req.Timeout / time.Duration(min(len(scanIdx), 4))
	}
	for pos, i := range scanIdx {
		c := candidates[i]
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return Result{}, ctx.Err()
			}
			budget = true
			break
		}
		report := &coverage.Files[i]
		report.Status = "partial"
		report.Reason = ""
		one, cancel := context.WithTimeout(ctx, slice)
		scanCtx, _ := withSheetReports(withSheetFilter(one, req.Sheets))
		result.Metrics.XLSXFilesScanned++
		e := scan(scanCtx, c.physical, patterns, func(sheet, cell, source, text string) error {
			groups := []string{}
			matched := []string{}
			score := 0
			for _, g := range []struct {
				name     string
				patterns []string
			}{{"symbols", req.Symbols}, {"terms", req.Terms}} {
				hits := termHits(text, g.patterns, exact)
				if len(hits) == 0 {
					continue
				}
				groups = append(groups, g.name)
				matched = append(matched, hits...)
				score = max(score, matchSpecificity(text, hits))
			}
			if len(groups) == 0 {
				return nil
			}
			if matches >= req.MaxMatches {
				return errMatchBudget
			}
			matches++
			if result.Metrics.FirstAnchorMS == nil {
				ms := time.Since(started).Milliseconds()
				result.Metrics.FirstAnchorMS = &ms
			}
			anchors = append(anchors, Anchor{Kind: "config", Path: c.relative, Text: shortText(text), Groups: groups, MatchedTerms: matched, Workbook: &WorkbookLocation{Sheet: sheet, Cell: cell, Source: source}, specificity: score})
			return nil
		})
		if e == nil || errors.Is(e, errWorkbookBudget) {
			if sheets := sheetReportsFrom(scanCtx); len(sheets) > 0 {
				report.Sheets = sheets
			}
		}
		cancel()
		switch {
		case e == nil:
			report.Status = "complete"
			if !notedSheets && sheetsSkipped(report.Sheets) {
				notedSheets = true
				result.Unknowns = append(result.Unknowns, "未列入 --sheet 的工作表没有内容扫描。")
			}
		case errors.Is(e, errMatchBudget):
			budget = true
			report.Reason = "match_limit"
			for _, j := range scanIdx[pos+1:] {
				coverage.Files[j].Reason = "match_limit"
			}
		case errors.Is(e, context.DeadlineExceeded):
			budget = true
			report.Reason = "file_timeout"
			if ctx.Err() != nil {
				report.Reason = "total_timeout"
			}
		case errors.Is(e, errWorkbookBudget):
			budget = true
			report.Reason = "file_size_or_xml_limit"
			if lim, ok := asLimit(e); ok {
				report.Reason = lim.Kind
				report.LimitKind = lim.Kind
				report.LimitBytes = lim.Limit
				report.ActualBytes = lim.Actual
				report.BudgetBytes = lim.Budget
				report.Entry = lim.Entry
				report.Sheet = lim.Sheet
				if len(lim.Sheets) > 0 {
					report.Sheets = lim.Sheets
				}
				if lim.Kind == "sheet_limit" {
					report.Reason = "sheet_limit"
					copyFirstSheetLimit(report)
				}
			}
		default:
			if lim, ok := asLimit(e); ok && lim.Kind == "sheet_not_found" {
				report.Status = "partial"
				report.Reason = "sheet_not_found"
				report.Sheets = lim.Sheets
				result.Unknowns = append(result.Unknowns, "请求的工作表没有全部找到，见 sheets。未找到不是「表里没有该字段」。")
				continue
			}
			if len(scanIdx) == 1 {
				return Result{}, fmt.Errorf("读取 XLSX %s: %w", c.relative, e)
			}
			report.Status = "partial"
			report.Reason = "read_error"
			report.Detail = shortErr(e)
			readFailed = true
		}
		if errors.Is(e, errMatchBudget) {
			break
		}
	}
	if len(scanIdx) > 0 {
		last := scanIdx[len(scanIdx)-1]
		for i := last + 1; i < len(candidates); i++ {
			if coverage.Files[i].Status != "complete" {
				coverage.NextPath = candidates[i].relative
				break
			}
		}
	}
	donePaths := []string{}
	incomplete := false
	for _, f := range coverage.Files {
		if f.Status == "complete" {
			donePaths = append(donePaths, f.Path)
		} else {
			incomplete = true
		}
	}
	if incomplete && len(donePaths) > 0 {
		batch := req.MaxFiles
		if batch == 0 {
			batch = 1
		}
		if namedSingle && !explicitBatch && req.Cursor == "" && coverage.NextPath != "" {
			coverage.NextCommand = xlsxFollowCommand(req, "path", "", 0, coverage.NextPath)
		} else if req.dirScope || explicitBatch || req.Cursor != "" {
			coverage.ResumeCursor = encodeResumeCursor(donePaths)
			coverage.NextCommand = xlsxFollowCommand(req, "cursor", coverage.ResumeCursor, batch, "")
		}
	}
	if coverage.NextCommand == "" && coverage.NextPath != "" && incomplete {
		coverage.NextCommand = xlsxFollowCommand(req, "path", "", 0, coverage.NextPath)
	}
	coverage.ContentComplete = complete && !incomplete && !budget && !readFailed
	sort.SliceStable(anchors, func(i, j int) bool {
		a, b := anchors[i], anchors[j]
		if groupPriority(a.Groups) != groupPriority(b.Groups) {
			return groupPriority(a.Groups) < groupPriority(b.Groups)
		}
		return a.specificity > b.specificity
	})
	result.Metrics.RawMatches = matches
	result.Metrics.Truncated = budget || readFailed || len(anchors) > req.MaxAnchors
	anchors = projectWorkbookAnchors(anchors, req.MaxAnchors)
	result.Anchors = anchors
	result.Metrics.ProjectedAnchor = len(anchors)
	result.Metrics.ElapsedMS = time.Since(started).Milliseconds()
	if readFailed && len(anchors) == 0 && !budget {
		budget = true
	}
	switch {
	case budget:
		result.Status = StatusBudgetExceeded
		result.Unknowns = append(result.Unknowns, "XLSX 扫描不完整，请查看 workbook_coverage 中未完成文件及原因；先用 --path 点名或 !排除单个工作簿，不要先把 timeout 加到 10s 以上。不能当成全目录已扫描。")
	case len(anchors) == 0:
		result.Status = StatusNoCandidates
		result.Unknowns = append(result.Unknowns, "零字面命中只表示 unknown，不能写成表里没有该字段；近义写法、别名或未扫描文件仍可能存在。")
	default:
		result.Status = StatusCandidatesFound
	}
	if readFailed {
		result.Unknowns = append(result.Unknowns, "有工作簿读取失败，见 reason=read_error；失败文件没有挡住其它文件。")
	}
	return result, nil
}

func coverageHasDeferred(coverage *WorkbookCoverage) bool {
	for _, f := range coverage.Files {
		if f.Reason == reasonDeferred {
			return true
		}
	}
	return false
}

func sheetsSkipped(sheets []SheetScan) bool {
	for _, sheet := range sheets {
		if sheet.Reason == "not_requested" {
			return true
		}
	}
	return false
}

func copyFirstSheetLimit(report *WorkbookScan) {
	for _, sheet := range report.Sheets {
		if sheet.LimitKind == "" {
			continue
		}
		report.LimitKind = sheet.LimitKind
		report.LimitBytes = sheet.LimitBytes
		report.ActualBytes = sheet.ActualBytes
		report.BudgetBytes = sheet.BudgetBytes
		report.Entry = sheet.Entry
		report.Sheet = sheet.Name
		return
	}
}

func termHits(text string, patterns []string, exact bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range patterns {
		if p == "" || seen[p] {
			continue
		}
		ok := strings.Contains(text, p)
		if exact {
			ok = text == p
		}
		if ok {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func shortErr(err error) string {
	s := err.Error()
	r := []rune(s)
	if len(r) > 180 {
		return string(r[:180])
	}
	return s
}

func encodeResumeCursor(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return "cf1." + base64.RawURLEncoding.EncodeToString([]byte(strings.Join(paths, "\n")))
}

func decodeResumeCursor(s string) ([]string, error) {
	if !strings.HasPrefix(s, "cf1.") {
		return nil, errors.New("无法识别的续扫 cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, "cf1."))
	if err != nil {
		return nil, errors.New("无法识别的续扫 cursor")
	}
	out := []string{}
	for _, p := range strings.Split(string(raw), "\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
			return nil, errors.New("cursor 含非法路径")
		}
		for _, seg := range strings.Split(p, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return nil, errors.New("cursor 含非法路径")
			}
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("续扫 cursor 为空")
	}
	return out, nil
}

func xlsxFollowCommand(req normalizedRequest, mode, cursor string, maxFiles int, nextPath string) string {
	args := []string{"codefind", "--root", req.root, "--format", "xlsx"}
	for _, term := range req.Terms {
		args = append(args, "--term", term)
	}
	for _, symbol := range req.Symbols {
		args = append(args, "--symbol", symbol)
	}
	if mode == "path" {
		args = append(args, "--path", nextPath)
	} else {
		for _, p := range req.Paths {
			if p == "." && len(req.Paths) == 1 {
				continue
			}
			args = append(args, "--path", p)
		}
		if maxFiles <= 0 {
			maxFiles = 1
		}
		args = append(args, "--max-files", strconv.Itoa(maxFiles), "--cursor", cursor)
	}
	if req.Match == "exact" {
		args = append(args, "--match", "exact")
	}
	for _, sheet := range req.Sheets {
		args = append(args, "--sheet", sheet)
	}
	args = append(args, "--max-anchors", strconv.Itoa(req.MaxAnchors), "--max-matches", strconv.Itoa(req.MaxMatches), "--timeout", req.Timeout.String())
	return joinCommand(args)
}

func joinCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteCommandArg(arg)
	}
	return strings.Join(quoted, " ")
}

func quoteCommandArg(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		if r <= ' ' || strings.ContainsRune("`\"'\\&|;<>()*?[]{}!#~", r) {
			return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
		}
	}
	return s
}

var errMatchBudget = errors.New("match budget exceeded")

// Preserve relevance order inside each sheet, but distribute scarce output
// slots across workbooks and their sheets. This does not expand scan budgets.
func projectWorkbookAnchors(anchors []Anchor, limit int) []Anchor {
	type workbookBucket struct {
		sheets []string
		rows   map[string][]Anchor
		next   int
	}
	books := []*workbookBucket{}
	byPath := map[string]*workbookBucket{}
	for _, a := range anchors {
		b := byPath[a.Path]
		if b == nil {
			b = &workbookBucket{rows: map[string][]Anchor{}}
			byPath[a.Path] = b
			books = append(books, b)
		}
		sheet := ""
		if a.Workbook != nil {
			sheet = a.Workbook.Sheet
		}
		if _, ok := b.rows[sheet]; !ok {
			b.sheets = append(b.sheets, sheet)
		}
		b.rows[sheet] = append(b.rows[sheet], a)
	}
	out := make([]Anchor, 0, min(max(limit, 0), len(anchors)))
	for len(out) < limit {
		progress := false
		for _, b := range books {
			for tried := 0; tried < len(b.sheets); tried++ {
				sheet := b.sheets[b.next]
				b.next = (b.next + 1) % len(b.sheets)
				if len(b.rows[sheet]) == 0 {
					continue
				}
				out = append(out, b.rows[sheet][0])
				b.rows[sheet] = b.rows[sheet][1:]
				progress = true
				break
			}
			if len(out) == limit {
				return out
			}
		}
		if !progress {
			break
		}
	}
	return out
}
