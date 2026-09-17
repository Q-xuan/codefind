package find

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLanguageSelection(t *testing.T) {
	requireRG(t)
	files := map[string]string{"main.go": "package p\nfunc Target() {}\n", "rules.proto": "message Target {}\n", "docs/rules.md": "Target\n", "config/rules.yaml": "name: Target\n", "config/items.csv": "id,name\n1,Target\n", "config/items.tsv": "id\tname\n1\tTarget\n", "node_modules/no.js": "Target\n", "vendor/no.lua": "Target\n", "bundle.min.js": "Target\n"}
	for _, lang := range sourceLanguages {
		if lang.name == "go" {
			continue
		}
		for _, ext := range lang.extensions {
			files[languageFixturePath(ext)] = "Target\n"
		}
	}
	root := makeRepo(t, files)
	for _, lang := range sourceLanguages {
		t.Run(lang.name, func(t *testing.T) {
			r, e := Find(context.Background(), Request{Root: root, Symbols: []string{"Target"}, Languages: []string{lang.name}, MaxAnchors: 50})
			if e != nil {
				t.Fatal(e)
			}
			if r.Metrics.RGCalls != 1 || !reflect.DeepEqual(r.Query.Languages, []string{lang.name}) {
				t.Fatal(r.Metrics, r.Query)
			}
			for _, a := range r.Anchors {
				if a.Path == "node_modules/no.js" || a.Path == "vendor/no.lua" || a.Path == "bundle.min.js" {
					t.Fatal(a.Path)
				}
				if a.Path != "main.go" && a.Syntax != nil {
					t.Fatal("non-Go syntax evidence", a)
				}
			}
			for _, other := range sourceLanguages {
				if other.name == "go" {
					continue
				}
				for _, ext := range other.extensions {
					if !contains(lang.extensions, ext) && hasPath(r.Anchors, languageFixturePath(ext)) {
						t.Fatal("unselected extension", ext)
					}
				}
			}
			for _, p := range []string{"rules.proto", "docs/rules.md", "config/rules.yaml", "config/items.csv", "config/items.tsv"} {
				if !hasPath(r.Anchors, p) {
					t.Fatal("missing domain evidence", p)
				}
			}
			if lang.name == "go" {
				if !hasPath(r.Anchors, "main.go") {
					t.Fatal("Go missing")
				}
			} else {
				if hasPath(r.Anchors, "main.go") {
					t.Fatal("Go scope expanded")
				}
				for _, ext := range lang.extensions {
					if !hasPath(r.Anchors, languageFixturePath(ext)) {
						t.Fatal("missing", ext)
					}
				}
			}
		})
	}
	r, e := Find(context.Background(), Request{Root: root, Terms: []string{"Target"}, Languages: []string{"lua", "ts"}, MaxAnchors: 50})
	if e != nil || !hasPath(r.Anchors, "src/target.lua") || !hasPath(r.Anchors, "src/target.ts") || hasPath(r.Anchors, "src/target.cs") {
		t.Fatalf("r=%+v err=%v", r, e)
	}
	r, e = Find(context.Background(), Request{Root: root, Terms: []string{"Target"}})
	if e != nil || hasPath(r.Anchors, "src/target.lua") {
		t.Fatal("default changed", e)
	}
	r, e = Find(context.Background(), Request{Root: root, Symbols: []string{"Target"}, Languages: []string{"all"}, MaxAnchors: 50})
	if e != nil {
		t.Fatal(e)
	}
	for _, lang := range sourceLanguages {
		if lang.name == "go" {
			continue
		}
		for _, ext := range lang.extensions {
			if !hasPath(r.Anchors, languageFixturePath(ext)) {
				t.Fatal("all missing", ext)
			}
		}
	}
	goEvidence := false
	for _, a := range r.Anchors {
		if a.Path == "main.go" && a.Syntax != nil {
			goEvidence = true
		}
	}
	if !goEvidence {
		t.Fatal("Go AST lost in mixed-language search")
	}
}

func languageFixturePath(ext string) string {
	// Windows cannot hold target.c and target.C as distinct fixture files.
	if ext == ".C" {
		return "src/uppercase.C"
	}
	return "src/target" + ext
}

func TestLanguageNormalization(t *testing.T) {
	got, e := normalizeLanguages([]string{" C# ", "cs", "C++", "javascript", "typescript"})
	if e != nil || !reflect.DeepEqual(got, []string{"csharp", "cpp", "js", "ts"}) {
		t.Fatal(got, e)
	}
	all, e := normalizeLanguages([]string{"all", "go"})
	if e != nil || len(all) != len(sourceLanguages) {
		t.Fatal(all, e)
	}
	for _, req := range []Request{{Languages: []string{"unsupported"}}, {Languages: []string{""}}, {Format: "xlsx", Languages: []string{"go"}}} {
		_, e := Find(context.Background(), req)
		if !errors.Is(e, ErrInvalidRequest) {
			t.Fatal(e)
		}
	}
}

func TestNonGoSourceClassification(t *testing.T) {
	for _, p := range []string{"main.lua", "main.cs", "main.cpp", "main.tsx"} {
		if classify(p, "Target()") != "source" {
			t.Fatal(p)
		}
	}
	if classify("tests/main.lua", "Target()") != "test" {
		t.Fatal("test directory")
	}
}
