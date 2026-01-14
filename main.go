package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ==========================================
// 1. Domain Types & Constants
// ==========================================

const (
	MaxSnippets  = 10
	ContextChars = 10
)

// SearchResult は1つの検索語に対する結果を保持します
type SearchResult struct {
	Query    string
	Count    int
	Snippets []string
}

// Config は実行時の設定を保持します
type Config struct {
	InputFilePath string
	Queries       []string
}

// ==========================================
// 2. Business Logic (Pure Functions)
// ==========================================

// ParseArgs は実行引数と実行ファイル名から設定を生成します。
func ParseArgs(args []string, execPath string) (*Config, error) {
	if len(args) < 1 {
		return nil, errors.New("input file path is required")
	}

	inputFile := args[0]
	baseName := filepath.Base(execPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]

	// アンダースコアで分割 (例: AppName_Query1_Query2)
	parts := strings.Split(nameWithoutExt, "_")

	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid executable name format: %s (expected: AppName_Query1_Query2...)", baseName)
	}

	// 先頭(アプリ名)を除外した残りが検索クエリ
	queries := parts[1:]

	// 有効なクエリのみ抽出
	validQueries := make([]string, 0, len(queries))
	for _, q := range queries {
		if q != "" {
			validQueries = append(validQueries, q)
		}
	}

	if len(validQueries) == 0 {
		return nil, errors.New("no search queries found in executable name")
	}

	return &Config{
		InputFilePath: inputFile,
		Queries:       validQueries,
	}, nil
}

// SearchStream はストリームから文字列を検索します。
func SearchStream(r io.Reader, queries []string) (map[string]*SearchResult, error) {
	results := make(map[string]*SearchResult)
	for _, q := range queries {
		results[q] = &SearchResult{Query: q}
	}

	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		lineText := scanner.Text()

		// 最適化: ルーン変換はコストが高いため、いずれかのクエリがヒットした場合のみ行う
		// nilのままなら変換していない状態
		var lineRunes []rune

		for _, q := range queries {
			// 高速なバイト検索で事前チェック
			if !strings.Contains(lineText, q) {
				continue
			}

			res := results[q]
			res.Count++ // 行単位でカウント

			// スニペットが必要な場合のみルーン変換して抽出処理を行う
			if len(res.Snippets) < MaxSnippets {
				// 遅延初期化: この行で初めてスニペット抽出が必要になった時だけ変換
				if lineRunes == nil {
					lineRunes = []rune(lineText)
				}

				snippet := extractSnippet(lineRunes, q)
				res.Snippets = append(res.Snippets, snippet)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading stream: %w", err)
	}

	return results, nil
}

// extractSnippet は行の中からクエリを見つけ、前後10文字を切り出します
func extractSnippet(lineRunes []rune, query string) string {
	queryRunes := []rune(query)
	qLen := len(queryRunes)
	lineLen := len(lineRunes)

	// ルーン単位での位置特定
	idx := -1
	for i := 0; i <= lineLen-qLen; i++ {
		match := true
		for j := 0; j < qLen; j++ {
			if lineRunes[i+j] != queryRunes[j] {
				match = false
				break
			}
		}
		if match {
			idx = i
			break
		}
	}

	if idx == -1 {
		return "" // 事前のContainsチェックがあるため通常は到達しない
	}

	start := idx - ContextChars
	if start < 0 {
		start = 0
	}

	end := idx + qLen + ContextChars
	if end > lineLen {
		end = lineLen
	}

	return string(lineRunes[start:end])
}

// WriteResults は結果を指定されたWriterに出力します
func WriteResults(w io.Writer, results map[string]*SearchResult, queryOrder []string) {
	for _, q := range queryOrder {
		res, ok := results[q]
		if !ok {
			continue
		}

		fmt.Fprintf(w, "[%s]\n", res.Query)
		fmt.Fprintf(w, "該当数: %d\n", res.Count)

		for i, snippet := range res.Snippets {
			fmt.Fprintf(w, "%d:%s\n", i+1, snippet)
		}
		fmt.Fprintln(w, "-----------------------")
	}
}

// ==========================================
// 3. Application Wiring
// ==========================================

type AppContext struct {
	Args       []string
	ExecPath   string
	Stdout     io.Writer
	Stderr     io.Writer
	FileReader func(string) (io.ReadCloser, error)
}

func Run(ctx AppContext) int {
	logger := slog.New(slog.NewTextHandler(ctx.Stderr, nil))

	// 引数調整: os.Argsの先頭は通常実行パスだが、DIで渡される場合は注意が必要
	userArgs := ctx.Args
	if len(userArgs) > 0 {
		// 一般的なCLIの振る舞いとして、最初の要素(プログラム名)をスキップして実引数を取得
		userArgs = userArgs[1:]
	}

	config, err := ParseArgs(userArgs, ctx.ExecPath)
	if err != nil {
		logger.Error("Configuration error", "error", err)
		return 1
	}

	f, err := ctx.FileReader(config.InputFilePath)
	if err != nil {
		logger.Error("Failed to open file", "path", config.InputFilePath, "error", err)
		return 1
	}
	defer f.Close()

	results, err := SearchStream(f, config.Queries)
	if err != nil {
		logger.Error("Search failed", "error", err)
		return 1
	}

	WriteResults(ctx.Stdout, results, config.Queries)

	return 0
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}

	ctx := AppContext{
		Args:     os.Args,
		ExecPath: exe,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		FileReader: func(path string) (io.ReadCloser, error) {
			return os.Open(path)
		},
	}

	os.Exit(Run(ctx))
}
