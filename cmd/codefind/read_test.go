package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadCLIInvalidAndHelp(t *testing.T) {
	for _, args := range [][]string{{"--timeout", "nope"}, {"--unknown"}, {"extra"}, {}} {
		var out, errout bytes.Buffer
		code := runRead(args, &out, &errout)
		var result map[string]any
		if e := json.Unmarshal(out.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		if code != 2 || result["status"] != "invalid_request" {
			t.Fatalf("code=%d result=%v", code, result)
		}
	}
	var out bytes.Buffer
	if code := runRead([]string{"--help"}, &out, &out); code != 0 || out.Len() == 0 {
		t.Fatal(code)
	}
	text := out.String()
	for _, want := range []string{"--range", "--field", "更稳", "unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("read help missing %q in %s", want, text)
		}
	}
}

func TestReadHelpOutputFailure(t *testing.T) {
	var diagnostic bytes.Buffer
	if code := runRead([]string{"--help"}, brokenWriter{}, &diagnostic); code != 1 || diagnostic.Len() == 0 {
		t.Fatal(code)
	}
}
