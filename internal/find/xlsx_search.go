package find

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type WorkbookCoverage struct {
	DiscoveryComplete bool           `json:"discovery_complete"`
	Files             []WorkbookScan `json:"files"`
}
type WorkbookScan struct {
	Path           string `json:"path"`
	Status         string `json:"status"` // pending, complete, partial
	Reason         string `json:"reason,omitempty"`
	MetadataStatus string `json:"metadata_status"`
}
type workbookCandidate struct {
	physical, relative string
	size               int64
	score              int
	metadata           string
}

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
func workbookNameScore(ctx context.Context, filename string, patterns []string) (int, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return 0, err
	}
	defer z.Close()
	a := xlsxArchive{ctx: ctx, remaining: 1 << 20, files: map[string]*zip.File{}}
	for _, f := range z.File {
		if f.Name == "xl/workbook.xml" {
			a.files[f.Name] = f
		}
	}
	score := 0
	err = a.decode("xl/workbook.xml", func(d *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local != "sheet" {
			return nil
		}
		for _, attr := range s.Attr {
			if attr.Name.Local == "name" {
				score = max(score, nameScore(attr.Value, patterns))
			}
		}
		return nil
	})
	return score, err
}

func discoverWorkbooks(ctx context.Context, req normalizedRequest) ([]workbookCandidate, bool, error) {
	candidates := []workbookCandidate{}
	seen := map[string]bool{}
	for _, scope := range req.Paths {
		base := filepath.Join(req.root, filepath.FromSlash(scope))
		err := filepath.WalkDir(base, func(filename string, d fs.DirEntry, walkErr error) error {
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
				return nil
			}
			if !strings.EqualFold(filepath.Ext(filename), ".xlsx") || strings.HasPrefix(d.Name(), "~$") {
				return nil
			}
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
			seen[physical] = true
			if len(candidates) >= maxWorkbooks {
				return errWorkbookBudget
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(req.root, filename)
			if err != nil {
				return err
			}
			candidates = append(candidates, workbookCandidate{physical: physical, relative: filepath.ToSlash(relative), size: info.Size(), metadata: "pending"})
			return nil
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkbookBudget) {
				return candidates, false, nil
			}
			return nil, false, err
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
	// Metadata gets at most 20% of the request; each workbook gets at most 50ms.
	metaCtx, metaCancel := context.WithTimeout(ctx, req.Timeout/5)
	for i := range candidates {
		c := &candidates[i]
		c.score = nameScore(filepath.Base(c.relative), patterns)
		if metaCtx.Err() != nil || c.size > maxWorkbookBytes {
			c.metadata = "skipped"
			continue
		}
		one, cancel := context.WithTimeout(metaCtx, 50*time.Millisecond)
		score, e := workbookNameScore(one, c.physical, patterns)
		cancel()
		if e == nil {
			c.score += score
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
		if a.size != b.size {
			return a.size < b.size
		}
		return a.relative < b.relative
	})
	coverage := &WorkbookCoverage{DiscoveryComplete: complete, Files: []WorkbookScan{}}
	for _, c := range candidates {
		coverage.Files = append(coverage.Files, WorkbookScan{Path: c.relative, Status: "pending", Reason: "total_timeout", MetadataStatus: c.metadata})
	}
	result.WorkbookCoverage = coverage
	result.Unknowns = append(result.Unknowns, "XLSX 仅搜索单元格存储值和传统批注；不重算公式、不搜索图片或线程评论。")
	anchors := []Anchor{}
	matches := 0
	budget := !complete
	// A single file retains the whole request; multiple files share time slices.
	slice := req.Timeout / time.Duration(min(max(len(candidates), 1), 4))
	for i, c := range candidates {
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
		result.Metrics.XLSXFilesScanned++
		e := scan(one, c.physical, patterns, func(sheet, cell, source, text string) error {
			groups := []string{}
			score := 0
			for _, g := range []struct {
				name     string
				patterns []string
			}{{"symbols", req.Symbols}, {"terms", req.Terms}} {
				for _, p := range g.patterns {
					if strings.Contains(text, p) {
						groups = append(groups, g.name)
						score = max(score, matchSpecificity(text, g.patterns))
						break
					}
				}
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
			anchors = append(anchors, Anchor{Kind: "config", Path: c.relative, Text: shortText(text), Groups: groups, Workbook: &WorkbookLocation{Sheet: sheet, Cell: cell, Source: source}, specificity: score})
			return nil
		})
		cancel()
		switch {
		case e == nil:
			report.Status = "complete"
		case errors.Is(e, errMatchBudget):
			budget = true
			report.Reason = "match_limit"
			for j := i + 1; j < len(coverage.Files); j++ {
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
		default:
			return Result{}, fmt.Errorf("读取 XLSX %s: %w", c.relative, e)
		}
		if errors.Is(e, errMatchBudget) {
			break
		}
	}
	sort.SliceStable(anchors, func(i, j int) bool {
		a, b := anchors[i], anchors[j]
		if groupPriority(a.Groups) != groupPriority(b.Groups) {
			return groupPriority(a.Groups) < groupPriority(b.Groups)
		}
		return a.specificity > b.specificity
	})
	result.Metrics.RawMatches = matches
	result.Metrics.Truncated = budget || len(anchors) > req.MaxAnchors
	anchors = projectWorkbookAnchors(anchors, req.MaxAnchors)
	result.Anchors = anchors
	result.Metrics.ProjectedAnchor = len(anchors)
	result.Metrics.ElapsedMS = time.Since(started).Milliseconds()
	switch {
	case budget:
		result.Status = StatusBudgetExceeded
		result.Unknowns = append(result.Unknowns, "XLSX 扫描不完整，请查看 workbook_coverage 中未完成文件及原因；可缩小范围或增加 timeout 重试。")
	case len(anchors) == 0:
		result.Status = StatusNoCandidates
		result.Unknowns = append(result.Unknowns, "零命中不代表内容不存在；请核对查询词和支持的内容类型。")
	default:
		result.Status = StatusCandidatesFound
	}
	return result, nil
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
