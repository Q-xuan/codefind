package find

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Keep language support lexical. Adding a file extension does not imply a parser.
var sourceLanguages = []struct {
	name       string
	extensions []string
}{
	{"go", []string{".go"}},
	{"lua", []string{".lua"}},
	{"csharp", []string{".cs"}},
	{"c", []string{".c", ".h"}},
	{"cpp", []string{".cpp", ".cc", ".cxx", ".h", ".hpp", ".hh", ".hxx", ".C"}},
	{"js", []string{".js", ".jsx", ".mjs", ".cjs"}},
	{"ts", []string{".ts", ".tsx", ".mts", ".cts"}},
}

func normalizeLanguages(values []string) ([]string, error) {
	result := []string{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		switch v {
		case "c#", "cs":
			v = "csharp"
		case "c++":
			v = "cpp"
		case "javascript":
			v = "js"
		case "typescript":
			v = "ts"
		}
		if v == "all" {
			for _, lang := range sourceLanguages {
				if !contains(result, lang.name) {
					result = append(result, lang.name)
				}
			}
			continue
		}
		found := false
		for _, lang := range sourceLanguages {
			if v == lang.name {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("不支持 lang %q；可选 go、lua、csharp、c、cpp、js、ts、all", v)
		}
		if !contains(result, v) {
			result = append(result, v)
		}
	}
	if len(result) == 0 {
		result = []string{"go"}
	}
	return result, nil
}

func searchGlobs(languages []string) []string {
	globs := []string{}
	for _, name := range languages {
		for _, lang := range sourceLanguages {
			if lang.name == name {
				for _, ext := range lang.extensions {
					g := "*" + ext
					if !contains(globs, g) {
						globs = append(globs, g)
					}
				}
			}
		}
	}
	// Domain evidence stays available even when source language is narrowed.
	return append(globs, "*.proto", "*.md", "*.csv", "*.yaml", "*.yml", "!**/.git/**", "!**/vendor/**", "!**/node_modules/**", "!**/*.min.js", "!**/*.min.mjs", "!**/*.min.cjs")
}

func isLexicalSource(filename string) bool {
	ext := filepath.Ext(filename)
	for _, lang := range sourceLanguages {
		for _, supported := range lang.extensions {
			if ext == supported {
				return true
			}
		}
	}
	return false
}
