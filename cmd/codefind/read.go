package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Q-xuan/codefind/internal/find"
	"io"
	"time"
)

func runRead(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("codefind read", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var req find.ReadRequest
	var fields listFlag
	f.StringVar(&req.Root, "root", "", "授权根目录")
	f.StringVar(&req.File, "file", "", "root 内 XLSX 文件")
	f.StringVar(&req.Sheet, "sheet", "", "工作表名")
	f.StringVar(&req.Range, "range", "", "明确范围，例如 B6:AD20")
	f.StringVar(&req.Anchor, "anchor", "", "命中格，例如 D20；与 range 二选一")
	f.StringVar(&req.Strategy, "strategy", "", "adaptive（默认）、structure、row_headers、window")
	f.Var(&fields, "field", "目标字段名或别名，可重复")
	f.IntVar(&req.MaxCells, "max-cells", 96, "最多返回格子，上限 4096")
	f.IntVar(&req.MaxChars, "max-chars", 24000, "原始文本字符预算，上限 200000")
	f.DurationVar(&req.Timeout, "timeout", 2*time.Second, "总读取超时，上限 10s")
	if e := f.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			var help bytes.Buffer
			f.SetOutput(&help)
			f.PrintDefaults()
			if _, e := io.Copy(stdout, &help); e != nil {
				fmt.Fprintln(stderr, e)
				return 1
			}
			return 0
		}
		return writeError(stdout, stderr, "invalid_request", e, 2)
	}
	if f.NArg() != 0 {
		return writeError(stdout, stderr, "invalid_request", fmt.Errorf("unexpected positional arguments: %v", f.Args()), 2)
	}
	req.Fields = fields
	result, e := find.ReadWorkbook(context.Background(), req)
	if e != nil {
		if errors.Is(e, find.ErrInvalidRequest) {
			return writeError(stdout, stderr, "invalid_request", e, 2)
		}
		return writeError(stdout, stderr, "execution_error", e, 1)
	}
	if e := json.NewEncoder(stdout).Encode(result); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	return 0
}
