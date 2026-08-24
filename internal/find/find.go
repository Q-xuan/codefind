package find

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SchemaVersion = "codefind-result-v1"
	Version       = "0.1.0"
)

const (
	StatusCandidatesFound = "candidates_found"
	StatusNoCandidates    = "no_candidates"
	StatusBudgetExceeded  = "budget_exceeded"
	StatusToolUnavailable = "tool_unavailable"
)

const (
	defaultMaxAnchors = 12
	defaultMaxMatches = 2000
	defaultTimeout    = 2 * time.Second
	maxAllowedAnchors = 50
	maxAllowedMatches = 10000
	maxAllowedTimeout = 10 * time.Second
)

var matchLine = regexp.MustCompile(`^(.*?):(\d+):(.*)$`)

type Request struct {
	Root       string        `json:"root"`
	Terms      []string      `json:"terms"`
	Symbols    []string      `json:"symbols"`
	Paths      []string      `json:"paths"`
	MaxAnchors int           `json:"max_anchors"`
	MaxMatches int           `json:"max_matches"`
	Timeout    time.Duration `json:"timeout"`
}

type Query struct {
	Terms   []string `json:"terms"`
	Symbols []string `json:"symbols"`
	Paths   []string `json:"paths"`
}

type Anchor struct {
	Kind        string          `json:"kind"`
	Path        string          `json:"path"`
	Line        int             `json:"line"`
	Text        string          `json:"text"`
	Groups      []string        `json:"groups"`
	Syntax      *SyntaxEvidence `json:"syntax,omitempty"`
	pathRank    int
	specificity int
	paired      bool
}

type Metrics struct {
	AgentCalls         int    `json:"agent_calls"`
	RGCalls            int    `json:"rg_calls"`
	ElapsedMS          int64  `json:"elapsed_ms"`
	FirstAnchorMS      *int64 `json:"first_anchor_ms"`
	RawMatches         int    `json:"raw_matches"`
	ProjectedAnchor    int    `json:"projected_anchors"`
	Truncated          bool   `json:"truncated"`
	SyntaxFilesParsed  int    `json:"syntax_files_parsed"`
	SyntaxAnchors      int    `json:"syntax_anchors"`
	SyntaxParseErrors  int    `json:"syntax_parse_errors"`
	SyntaxFilesSkipped int    `json:"syntax_files_skipped"`
}

type Limits struct {
	MaxAnchors int   `json:"max_anchors"`
	MaxMatches int   `json:"max_matches"`
	TimeoutMS  int64 `json:"timeout_ms"`
}

type Result struct {
	SchemaVersion  string   `json:"schema_version"`
	Engine         string   `json:"engine"`
	Version        string   `json:"version"`
	Status         string   `json:"status"`
	Query          Query    `json:"query"`
	Anchors        []Anchor `json:"anchors"`
	Unknowns       []string `json:"unknowns"`
	Metrics        Metrics  `json:"metrics"`
	Limits         Limits   `json:"limits"`
	ExternalWrites int      `json:"external_writes"`
}

type rawMatch struct {
	path        string
	line        int
	text        string
	group       string
	pathRank    int
	specificity int
}

type normalizedRequest struct {
	Request
	root string
}

func Find(ctx context.Context, request Request) (Result, error) {
	started := time.Now()
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		SchemaVersion: SchemaVersion,
		Engine:        "codefind",
		Version:       Version,
		Query: Query{
			Terms:   normalized.Terms,
			Symbols: normalized.Symbols,
			Paths:   normalized.Paths,
		},
		Anchors:  []Anchor{},
		Unknowns: []string{},
		Metrics: Metrics{
			AgentCalls: 1,
		},
		Limits: Limits{
			MaxAnchors: normalized.MaxAnchors,
			MaxMatches: normalized.MaxMatches,
			TimeoutMS:  normalized.Timeout.Milliseconds(),
		},
		ExternalWrites: 0,
	}

	if _, err := exec.LookPath("rg"); err != nil {
		result.Status = StatusToolUnavailable
		result.Unknowns = append(result.Unknowns, "rg 不可用，未执行代码发现。")
		result.Metrics.ElapsedMS = time.Since(started).Milliseconds()
		return result, nil
	}

	searchCtx, cancel := context.WithTimeout(ctx, normalized.Timeout)
	defer cancel()

	groups := []struct {
		name  string
		items []string
	}{
		{name: "terms", items: normalized.Terms},
		{name: "symbols", items: normalized.Symbols},
	}

	all := make([]rawMatch, 0, normalized.MaxAnchors*2)
	budgetExceeded := false
	for _, group := range groups {
		if len(group.items) == 0 {
			continue
		}
		remaining := normalized.MaxMatches - len(all)
		if remaining <= 0 {
			budgetExceeded = true
			break
		}
		rows, truncated, first, err := runRG(searchCtx, normalized.root, normalized.Paths, group.name, group.items, remaining, started)
		result.Metrics.RGCalls++
		if result.Metrics.FirstAnchorMS == nil && first != nil {
			result.Metrics.FirstAnchorMS = first
		}
		all = append(all, rows...)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
				budgetExceeded = true
				break
			}
			return Result{}, err
		}
		if truncated {
			budgetExceeded = true
			break
		}
	}

	result.Metrics.RawMatches = len(all)
	anchors := collectAnchors(all)
	shortlistLimit := min(max(normalized.MaxAnchors*4, normalized.MaxAnchors), maxSyntaxFiles)
	shortlist := projectAnchors(anchors, shortlistLimit)
	syntax := enrichSyntax(searchCtx, normalized.root, shortlist, normalized.Symbols)
	result.Metrics.SyntaxFilesParsed = syntax.filesParsed
	result.Metrics.SyntaxAnchors = syntax.anchors
	result.Metrics.SyntaxParseErrors = syntax.parseErrors
	result.Metrics.SyntaxFilesSkipped = syntax.filesSkipped
	if syntax.budgetExceeded {
		budgetExceeded = true
	}
	result.Anchors = projectAnchors(shortlist, normalized.MaxAnchors)
	result.Metrics.ProjectedAnchor = len(result.Anchors)
	result.Metrics.Truncated = budgetExceeded || len(all) > normalized.MaxAnchors
	result.Metrics.ElapsedMS = time.Since(started).Milliseconds()

	switch {
	case budgetExceeded:
		result.Status = StatusBudgetExceeded
		result.Unknowns = append(result.Unknowns, "搜索达到时间或扫描预算，当前候选可能不完整。")
	case len(result.Anchors) == 0:
		result.Status = StatusNoCandidates
		result.Unknowns = append(result.Unknowns, "0 命中只表示 unknown；请更换稳定 symbol 或扩大已授权范围后回读源码。")
	default:
		result.Status = StatusCandidatesFound
	}
	return result, nil
}

func normalizeRequest(request Request) (normalizedRequest, error) {
	request.Terms = cleanList(request.Terms)
	request.Symbols = cleanList(request.Symbols)
	request.Paths = cleanList(request.Paths)
	if len(request.Terms) == 0 && len(request.Symbols) == 0 {
		return normalizedRequest{}, errors.New("至少提供一个 --term 或 --symbol")
	}
	if request.Root == "" {
		return normalizedRequest{}, errors.New("--root 不能为空")
	}

	root, err := filepath.Abs(request.Root)
	if err != nil {
		return normalizedRequest{}, fmt.Errorf("解析 root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return normalizedRequest{}, fmt.Errorf("解析 root 物理路径: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return normalizedRequest{}, fmt.Errorf("root 不是可读目录: %s", request.Root)
	}

	if len(request.Paths) == 0 {
		request.Paths = []string{"."}
	}
	paths := make([]string, 0, len(request.Paths))
	for _, value := range request.Paths {
		if filepath.IsAbs(value) {
			return normalizedRequest{}, fmt.Errorf("search path 必须是 root-relative: %s", value)
		}
		clean := filepath.Clean(value)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return normalizedRequest{}, fmt.Errorf("search path 逃逸 root: %s", value)
		}
		physical, err := filepath.EvalSymlinks(filepath.Join(root, clean))
		if err != nil {
			return normalizedRequest{}, fmt.Errorf("解析 search path %s: %w", value, err)
		}
		if !inside(root, physical) {
			return normalizedRequest{}, fmt.Errorf("search path 物理路径逃逸 root: %s", value)
		}
		if stat, err := os.Stat(physical); err != nil || !stat.IsDir() {
			return normalizedRequest{}, fmt.Errorf("search path 不是目录: %s", value)
		}
		paths = append(paths, filepath.ToSlash(clean))
	}
	request.Paths = cleanList(paths)

	if request.MaxAnchors == 0 {
		request.MaxAnchors = defaultMaxAnchors
	}
	if request.MaxAnchors < 1 || request.MaxAnchors > maxAllowedAnchors {
		return normalizedRequest{}, fmt.Errorf("max anchors 必须在 1..%d", maxAllowedAnchors)
	}
	if request.MaxMatches == 0 {
		request.MaxMatches = defaultMaxMatches
	}
	if request.MaxMatches < request.MaxAnchors || request.MaxMatches > maxAllowedMatches {
		return normalizedRequest{}, fmt.Errorf("max matches 必须在 max anchors..%d", maxAllowedMatches)
	}
	if request.Timeout == 0 {
		request.Timeout = defaultTimeout
	}
	if request.Timeout < time.Millisecond || request.Timeout > maxAllowedTimeout {
		return normalizedRequest{}, fmt.Errorf("timeout 必须在 1ms..%s", maxAllowedTimeout)
	}
	return normalizedRequest{Request: request, root: root}, nil
}

func runRG(ctx context.Context, root string, paths []string, group string, patterns []string, maxMatches int, started time.Time) ([]rawMatch, bool, *int64, error) {
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{
		"--fixed-strings",
		"--line-number",
		"--with-filename",
		"--no-heading",
		"--color", "never",
		"--max-columns", "300",
		"--max-columns-preview",
	}
	for _, glob := range []string{
		"*.go", "*.proto", "*.md", "*.csv", "*.yaml", "*.yml",
		"!**/.git/**", "!**/vendor/**", "!**/node_modules/**", "!**/*.min.js",
	} {
		args = append(args, "--glob", glob)
	}
	for _, pattern := range patterns {
		args = append(args, "-e", pattern)
	}
	args = append(args, "--")
	args = append(args, paths...)

	cmd := exec.CommandContext(groupCtx, "rg", args...)
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, nil, fmt.Errorf("打开 rg stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, false, nil, fmt.Errorf("启动 rg: %w", err)
	}

	rows := make([]rawMatch, 0, min(maxMatches, 128))
	var first *int64
	truncated := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(rows) >= maxMatches {
			truncated = true
			cancel()
			break
		}
		row, ok := parseMatch(scanner.Text(), group, paths, patterns)
		if !ok {
			continue
		}
		if first == nil {
			elapsed := time.Since(started).Milliseconds()
			first = &elapsed
		}
		rows = append(rows, row)
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if scanErr != nil && !truncated {
		return nil, false, first, fmt.Errorf("读取 rg 输出: %w", scanErr)
	}
	if truncated {
		return rows, true, first, nil
	}
	if ctx.Err() != nil {
		return rows, true, first, ctx.Err()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			return rows, false, first, nil
		}
		return nil, false, first, fmt.Errorf("rg 失败: %s", strings.TrimSpace(stderr.String()))
	}
	return rows, false, first, nil
}

func parseMatch(line string, group string, paths []string, patterns []string) (rawMatch, bool) {
	parts := matchLine.FindStringSubmatch(line)
	if len(parts) != 4 {
		return rawMatch{}, false
	}
	lineNumber, err := strconv.Atoi(parts[2])
	if err != nil || lineNumber < 1 {
		return rawMatch{}, false
	}
	path := filepath.ToSlash(filepath.Clean(parts[1]))
	if path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
		return rawMatch{}, false
	}
	text := shortText(parts[3])
	return rawMatch{
		path:        path,
		line:        lineNumber,
		text:        text,
		group:       group,
		pathRank:    pathPriority(path, paths),
		specificity: matchSpecificity(text, patterns),
	}, true
}

func collectAnchors(rows []rawMatch) []Anchor {
	byKey := make(map[string]*Anchor, len(rows))
	for _, row := range rows {
		key := fmt.Sprintf("%s:%d:%s", row.path, row.line, row.text)
		anchor, ok := byKey[key]
		if !ok {
			anchor = &Anchor{
				Kind:        classify(row.path, row.text),
				Path:        row.path,
				Line:        row.line,
				Text:        row.text,
				Groups:      []string{},
				pathRank:    row.pathRank,
				specificity: row.specificity,
			}
			byKey[key] = anchor
		}
		if row.specificity > anchor.specificity {
			anchor.specificity = row.specificity
		}
		if !contains(anchor.Groups, row.group) {
			anchor.Groups = append(anchor.Groups, row.group)
		}
	}
	anchors := make([]Anchor, 0, len(byKey))
	for _, anchor := range byKey {
		anchors = append(anchors, *anchor)
	}
	markEvidencePairs(anchors)
	return anchors
}

func projectAnchors(anchors []Anchor, maxAnchors int) []Anchor {
	sort.Slice(anchors, func(i, j int) bool {
		left, right := anchors[i], anchors[j]
		if groupPriority(left.Groups) != groupPriority(right.Groups) {
			return groupPriority(left.Groups) < groupPriority(right.Groups)
		}
		if contentPriority(left.Text) != contentPriority(right.Text) {
			return contentPriority(left.Text) < contentPriority(right.Text)
		}
		if left.specificity != right.specificity {
			return left.specificity > right.specificity
		}
		if syntaxPriority(left.Syntax) != syntaxPriority(right.Syntax) {
			return syntaxPriority(left.Syntax) < syntaxPriority(right.Syntax)
		}
		if left.paired != right.paired {
			return left.paired
		}
		if left.pathRank != right.pathRank {
			return left.pathRank < right.pathRank
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Text < right.Text
	})
	buckets := make(map[string][]Anchor)
	for _, anchor := range anchors {
		buckets[anchor.Kind] = append(buckets[anchor.Kind], anchor)
	}
	for kind, values := range buckets {
		buckets[kind] = uniquePathFirst(values)
	}
	order := []string{"test", "source", "consumer", "protocol", "config", "docs", "history", "generated"}
	projected := make([]Anchor, 0, min(maxAnchors, len(anchors)))
	for len(projected) < maxAnchors {
		progress := false
		for _, kind := range order {
			values := buckets[kind]
			if len(values) == 0 {
				continue
			}
			projected = append(projected, values[0])
			buckets[kind] = values[1:]
			progress = true
			if len(projected) == maxAnchors {
				break
			}
		}
		if !progress {
			break
		}
	}
	return projected
}

func markEvidencePairs(anchors []Anchor) {
	tests := make(map[string]struct{})
	production := make(map[string]struct{})
	for _, anchor := range anchors {
		key, isTest, ok := evidencePairKey(anchor.Path)
		if !ok {
			continue
		}
		if isTest {
			tests[key] = struct{}{}
		} else {
			production[key] = struct{}{}
		}
	}
	for index := range anchors {
		key, isTest, ok := evidencePairKey(anchors[index].Path)
		if !ok {
			continue
		}
		if isTest {
			_, anchors[index].paired = production[key]
		} else {
			_, anchors[index].paired = tests[key]
		}
	}
}

func evidencePairKey(path string) (string, bool, bool) {
	path = filepath.ToSlash(path)
	if strings.HasSuffix(path, "_test.go") {
		return strings.TrimSuffix(path, "_test.go"), true, true
	}
	if strings.HasSuffix(path, ".go") {
		return strings.TrimSuffix(path, ".go"), false, true
	}
	return "", false, false
}

func classify(path string, text string) string {
	lowerPath := strings.ToLower(filepath.ToSlash(path))
	lowerText := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.HasSuffix(lowerPath, "_test.go") || strings.Contains(lowerPath, "/test/") || strings.Contains(lowerPath, "/tests/"):
		return "test"
	case strings.HasSuffix(lowerPath, ".pb.go") || strings.HasSuffix(lowerPath, "_gen.go") || strings.Contains(lowerPath, "/generated/") || strings.HasPrefix(filepath.Base(lowerPath), "zz_"):
		return "generated"
	case strings.HasSuffix(lowerPath, ".proto") || strings.Contains(lowerPath, "/proto/"):
		return "protocol"
	case strings.HasSuffix(lowerPath, ".csv") || strings.HasSuffix(lowerPath, ".yaml") || strings.HasSuffix(lowerPath, ".yml") || strings.Contains(lowerPath, "data/tables/"):
		return "config"
	case strings.HasSuffix(lowerPath, ".md") || strings.HasPrefix(lowerPath, "docs/"):
		return "docs"
	case strings.HasPrefix(lowerText, "func ") || strings.HasPrefix(lowerText, "type ") || strings.HasPrefix(lowerText, "const ") || strings.HasPrefix(lowerText, "var "):
		return "source"
	default:
		return "consumer"
	}
}

func groupPriority(groups []string) int {
	if contains(groups, "symbols") {
		return 0
	}
	return 1
}

func contentPriority(text string) int {
	if strings.HasPrefix(strings.TrimSpace(text), "//") {
		return 1
	}
	return 0
}

func uniquePathFirst(anchors []Anchor) []Anchor {
	seen := make(map[string]struct{}, len(anchors))
	first := make([]Anchor, 0, len(anchors))
	rest := make([]Anchor, 0, len(anchors))
	for _, anchor := range anchors {
		if _, ok := seen[anchor.Path]; ok {
			rest = append(rest, anchor)
			continue
		}
		seen[anchor.Path] = struct{}{}
		first = append(first, anchor)
	}
	return append(first, rest...)
}

func pathPriority(path string, roots []string) int {
	for index, root := range roots {
		root = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(root)), "/")
		if root == "." || path == root || strings.HasPrefix(path, root+"/") {
			return index
		}
	}
	return len(roots)
}

func matchSpecificity(text string, patterns []string) int {
	best := 0
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) && len([]rune(pattern)) > best {
			best = len([]rune(pattern))
		}
	}
	return best
}

func cleanList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func inside(root string, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func shortText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\t", " "))
	runes := []rune(value)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return value
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
