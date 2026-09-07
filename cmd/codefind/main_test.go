package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/Q-xuan/codefind/internal/find"
)

func TestErrorsAreJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		failure error
		status  string
		code    int
	}{
		{"duration", []string{"--timeout", "nope"}, nil, "invalid_request", 2},
		{"unknown", []string{"--unknown"}, nil, "invalid_request", 2},
		{"missing", []string{"--root"}, nil, "invalid_request", 2},
		{"positional", []string{"extra"}, nil, "invalid_request", 2},
		{"validation", nil, find.ErrInvalidRequest, "invalid_request", 2},
		{"execution", nil, errors.New("rg failed"), "execution_error", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			code := run(tc.args, &out, &diagnostics, func(context.Context, find.Request) (find.Result, error) {
				if tc.failure == nil {
					t.Fatal("search called after invalid arguments")
				}
				return find.Result{}, tc.failure
			})
			var payload map[string]any
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatal(err, out.String())
			}
			if code != tc.code || payload["status"] != tc.status || payload["schema_version"] != "codefind-error-v1" || diagnostics.Len() != 0 {
				t.Fatalf("code=%d payload=%v stderr=%s", code, payload, diagnostics.String())
			}
		})
	}
}

func TestHelpVersionAndSuccess(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"--root", ".", "--symbol", "Find"}} {
		var out bytes.Buffer
		code := run(args, &out, io.Discard, func(_ context.Context, req find.Request) (find.Result, error) {
			if req.Root != "." || len(req.Symbols) != 1 {
				t.Fatalf("request=%+v", req)
			}
			return find.Result{Status: find.StatusCandidatesFound}, nil
		})
		if code != 0 || out.Len() == 0 {
			t.Fatalf("args=%v code=%d output=%s", args, code, out.String())
		}
	}
}

type brokenWriter struct{}

func TestLanguageFlags(t *testing.T) {
	code := run([]string{"--lang", "lua", "--lang", "ts"}, io.Discard, io.Discard, func(_ context.Context, req find.Request) (find.Result, error) {
		if len(req.Languages) != 2 || req.Languages[0] != "lua" || req.Languages[1] != "ts" {
			t.Fatal(req.Languages)
		}
		return find.Result{}, nil
	})
	if code != 0 {
		t.Fatal(code)
	}
}

func TestEncodingFlag(t *testing.T) {
	for _, value := range []string{"auto", "utf-8", "gbk", "gb18030"} {
		code := run([]string{"--encoding", value}, io.Discard, io.Discard, func(_ context.Context, req find.Request) (find.Result, error) {
			if req.Encoding != value {
				t.Fatalf("encoding=%q", req.Encoding)
			}
			return find.Result{}, nil
		})
		if code != 0 {
			t.Fatalf("code=%d", code)
		}
	}
}

func TestXLSXFlag(t *testing.T) {
	code := run([]string{"--format", "xlsx"}, io.Discard, io.Discard, func(_ context.Context, req find.Request) (find.Result, error) {
		if req.Format != "xlsx" {
			t.Fatal(req.Format)
		}
		return find.Result{}, nil
	})
	if code != 0 {
		t.Fatal(code)
	}
}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestOutputFailures(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}, {"--unknown"}, {}} {
		var diagnostics bytes.Buffer
		if code := run(args, brokenWriter{}, &diagnostics, func(context.Context, find.Request) (find.Result, error) { return find.Result{}, nil }); code != 1 || diagnostics.Len() == 0 {
			t.Fatalf("code=%d stderr=%s", code, diagnostics.String())
		}
	}
}
