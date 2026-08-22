package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	var terms listFlag
	var symbols listFlag
	var paths listFlag
	root := flag.String("root", "", "要搜索的仓库根目录")
	maxAnchors := flag.Int("max-anchors", 12, "最多输出多少个候选锚点")
	maxMatches := flag.Int("max-matches", 2000, "最多扫描多少条原始匹配")
	timeout := flag.Duration("timeout", 2*time.Second, "本次搜索总超时")
	showVersion := flag.Bool("version", false, "输出版本")
	flag.Var(&terms, "term", "领域词、动作词或历史别名，可重复")
	flag.Var(&symbols, "symbol", "候选 symbol 或 test 词，可重复")
	flag.Var(&paths, "path", "root 内的搜索目录，可重复；默认 .")
	flag.Parse()

	if *showVersion {
		fmt.Println(find.Version)
		return
	}
	result, err := find.Find(context.Background(), find.Request{
		Root:       *root,
		Terms:      terms,
		Symbols:    symbols,
		Paths:      paths,
		MaxAnchors: *maxAnchors,
		MaxMatches: *maxMatches,
		Timeout:    *timeout,
	})
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema_version": "codefind-error-v1",
			"status":         "invalid_request",
			"error":          err.Error(),
		})
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
