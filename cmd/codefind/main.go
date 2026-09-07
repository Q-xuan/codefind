package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	flagpkg "flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Q-xuan/codefind/internal/find"
)

type listFlag []string

func (f *listFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *listFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, find.Find))
}

func run(args []string, stdout, stderr io.Writer, search func(context.Context, find.Request) (find.Result, error)) int {
	if len(args) > 0 && args[0] == "read" {
		return runRead(args[1:], stdout, stderr)
	}
	flag := flagpkg.NewFlagSet("codefind", flagpkg.ContinueOnError)
	flag.SetOutput(io.Discard)
	var terms listFlag
	var symbols listFlag
	var paths listFlag
	var languages listFlag
	root := flag.String("root", "", "要搜索的仓库根目录")
	format := flag.String("format", "text", "搜索格式：text 或 xlsx")
	encoding := flag.String("encoding", "auto", "文件编码：auto、utf-8、gbk、gb18030；auto 不猜测 GBK")
	maxAnchors := flag.Int("max-anchors", 12, "最多输出多少个候选锚点")
	maxMatches := flag.Int("max-matches", 2000, "最多扫描多少条原始匹配")
	timeout := flag.Duration("timeout", 2*time.Second, "本次搜索总超时")
	showVersion := flag.Bool("version", false, "输出版本")
	flag.Var(&terms, "term", "领域词、动作词或历史别名，可重复")
	flag.Var(&symbols, "symbol", "候选 symbol 或 test 词，可重复")
	flag.Var(&paths, "path", "root 内的搜索目录，可重复；默认 .")
	flag.Var(&languages, "lang", "源码语言，可重复：go（默认）、lua、csharp、c、cpp、js、ts、all；保留协议/配置/文档")
	if err := flag.Parse(args); err != nil {
		if errors.Is(err, flagpkg.ErrHelp) {
			var help bytes.Buffer
			flag.SetOutput(&help)
			flag.PrintDefaults()
			if _, err := io.Copy(stdout, &help); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		}
		return writeError(stdout, stderr, "invalid_request", err, 2)
	}
	if flag.NArg() != 0 {
		return writeError(stdout, stderr, "invalid_request", fmt.Errorf("unexpected positional arguments: %v", flag.Args()), 2)
	}

	if *showVersion {
		if _, err := fmt.Fprintln(stdout, find.Version); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	result, err := search(context.Background(), find.Request{
		Root:       *root,
		Format:     *format,
		Encoding:   *encoding,
		Terms:      terms,
		Symbols:    symbols,
		Paths:      paths,
		Languages:  languages,
		MaxAnchors: *maxAnchors,
		MaxMatches: *maxMatches,
		Timeout:    *timeout,
	})
	if err != nil {
		if errors.Is(err, find.ErrInvalidRequest) {
			return writeError(stdout, stderr, "invalid_request", err, 2)
		}
		return writeError(stdout, stderr, "execution_error", err, 1)
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func writeError(stdout, stderr io.Writer, status string, cause error, code int) int {
	if err := json.NewEncoder(stdout).Encode(map[string]any{
		"schema_version": "codefind-error-v1",
		"status":         status,
		"error":          cause.Error(),
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return code
}
