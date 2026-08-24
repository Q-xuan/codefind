package find

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxSyntaxFiles     = 64
	maxSyntaxFileBytes = 1 << 20
)

const (
	SyntaxRoleDefinition = "definition"
	SyntaxRoleCall       = "call"
	SyntaxRoleReference  = "reference"
	syntaxAuthority      = "go_ast_syntax"
)

var goIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

type SyntaxEvidence struct {
	Role      string `json:"role"`
	Symbol    string `json:"symbol"`
	Authority string `json:"authority"`
}

type syntaxMetrics struct {
	filesParsed    int
	anchors        int
	parseErrors    int
	filesSkipped   int
	budgetExceeded bool
}

type syntaxHit struct {
	role   string
	symbol string
}

func enrichSyntax(ctx context.Context, root string, anchors []Anchor, patterns []string) syntaxMetrics {
	symbols := syntaxSymbols(patterns)
	if len(symbols) == 0 {
		return syntaxMetrics{}
	}

	byPath := make(map[string][]int)
	paths := make([]string, 0, len(anchors))
	for index := range anchors {
		if !strings.HasSuffix(strings.ToLower(anchors[index].Path), ".go") || anchors[index].Kind == "generated" {
			continue
		}
		if _, ok := byPath[anchors[index].Path]; !ok {
			paths = append(paths, anchors[index].Path)
		}
		byPath[anchors[index].Path] = append(byPath[anchors[index].Path], index)
	}

	metrics := syntaxMetrics{}
	for _, relative := range paths {
		if ctx.Err() != nil {
			metrics.filesSkipped += len(paths) - metrics.filesParsed - metrics.filesSkipped
			metrics.budgetExceeded = true
			break
		}
		if metrics.filesParsed+metrics.filesSkipped >= maxSyntaxFiles {
			metrics.filesSkipped += len(paths) - metrics.filesParsed - metrics.filesSkipped
			break
		}
		physical, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !inside(root, physical) {
			metrics.filesSkipped++
			continue
		}
		info, err := os.Stat(physical)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSyntaxFileBytes {
			metrics.filesSkipped++
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, physical, nil, parser.SkipObjectResolution)
		if err != nil || file == nil {
			metrics.parseErrors++
			metrics.filesSkipped++
			continue
		}
		if ctx.Err() != nil {
			metrics.filesSkipped += len(paths) - metrics.filesParsed - metrics.filesSkipped
			metrics.budgetExceeded = true
			break
		}
		metrics.filesParsed++
		hits := collectSyntaxHits(fset, file, symbols)
		for _, index := range byPath[relative] {
			hit, ok := hits[anchors[index].Line]
			if !ok {
				continue
			}
			anchors[index].Syntax = &SyntaxEvidence{Role: hit.role, Symbol: hit.symbol, Authority: syntaxAuthority}
			metrics.anchors++
		}
	}
	return metrics
}

func syntaxSymbols(patterns []string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, pattern := range patterns {
		for _, segment := range strings.Split(pattern, "/") {
			identifiers := goIdentifier.FindAllString(segment, -1)
			for index := len(identifiers) - 1; index >= 0; index-- {
				identifier := identifiers[index]
				if !token.IsIdentifier(identifier) || token.Lookup(identifier).IsKeyword() {
					continue
				}
				result[identifier] = struct{}{}
				break
			}
		}
	}
	return result
}

func collectSyntaxHits(fset *token.FileSet, file *ast.File, symbols map[string]struct{}) map[int]syntaxHit {
	roles := make(map[*ast.Ident]string)
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			roles[value.Name] = SyntaxRoleDefinition
		case *ast.TypeSpec:
			roles[value.Name] = SyntaxRoleDefinition
		case *ast.ValueSpec:
			for _, name := range value.Names {
				roles[name] = SyntaxRoleDefinition
			}
		case *ast.CallExpr:
			switch function := value.Fun.(type) {
			case *ast.Ident:
				roles[function] = SyntaxRoleCall
			case *ast.SelectorExpr:
				roles[function.Sel] = SyntaxRoleCall
			}
		}
		return true
	})

	hits := make(map[int]syntaxHit)
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := symbols[identifier.Name]; !ok {
			return true
		}
		role := roles[identifier]
		if role == "" {
			role = SyntaxRoleReference
		}
		line := fset.Position(identifier.Pos()).Line
		candidate := syntaxHit{role: role, symbol: identifier.Name}
		current, exists := hits[line]
		if !exists || syntaxRolePriority(candidate.role) < syntaxRolePriority(current.role) ||
			(syntaxRolePriority(candidate.role) == syntaxRolePriority(current.role) && len(candidate.symbol) > len(current.symbol)) {
			hits[line] = candidate
		}
		return true
	})
	return hits
}

func syntaxPriority(evidence *SyntaxEvidence) int {
	if evidence == nil {
		return 3
	}
	return syntaxRolePriority(evidence.Role)
}

func syntaxRolePriority(role string) int {
	switch role {
	case SyntaxRoleDefinition:
		return 0
	case SyntaxRoleCall:
		return 1
	case SyntaxRoleReference:
		return 2
	default:
		return 3
	}
}
