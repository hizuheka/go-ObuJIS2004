package main

import (
	"bufio"
	"errors"
	"flag"
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
	MaxSnippets        = 10
	DefaultContextSize = 20 // デフォルトを20文字に変更
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
	ContextSize   int // コンテキスト文字数を保持するフィールドを追加
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

	// ContextSizeはここではデフォルト値を入れるか、呼び出し元で上書きする設計とする
	// ここでは構造体の初期化のみ行う
	return &Config{
		InputFilePath: inputFile,
		Queries:       validQueries,
		ContextSize:   DefaultContextSize,
	}, nil
}

// SearchStream はストリームから文字列を検索します。contextSizeを受け取るように変更
func SearchStream(r io.Reader, queries []string, contextSize int) (map[string]*SearchResult, error) {
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
				// contextSizeを渡す
				snippet := extractSnippet(lineRunes, q, contextSize)
				res.Snippets = append(res.Snippets, snippet)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading stream: %w", err)
	}

	return results, nil
}

// extractSnippet は指定されたcontextSizeに基づいて文字を切り出します
func extractSnippet(lineRunes []rune, query string, contextSize int) string {
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

	// 定数ContextCharsではなく、引数contextSizeを使用
	start := idx - contextSize
	if start < 0 {
		start = 0
	}

	end := idx + qLen + contextSize
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
	Args        []string
	ExecPath    string
	Stdout      io.Writer
	Stderr      io.Writer
	FileReader  func(string) (io.ReadCloser, error)
	FileCreator func(string) (io.WriteCloser, error)
}

func Run(ctx AppContext) int {
	logger := slog.New(slog.NewTextHandler(ctx.Stderr, nil))

	args := make([]string, len(ctx.Args))
	copy(args, ctx.Args)
	if len(args) > 0 {
		args = args[1:]
	}

	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	outputFile := fs.String("o", "", "Output file path (optional)")
	// コンテキストサイズを指定するフラグ -n を追加
	contextSize := fs.Int("n", DefaultContextSize, "Number of context characters (default 20)")

	if err := fs.Parse(args); err != nil {
		logger.Error("Flag parse error", "error", err)
		return 1
	}

	// 負の値が指定された場合のガード
	if *contextSize < 0 {
		logger.Error("Context size cannot be negative")
		return 1
	}

	remainingArgs := fs.Args()
	config, err := ParseArgs(remainingArgs, ctx.ExecPath)
	if err != nil {
		logger.Error("Configuration error", "error", err)
		return 1
	}

	// フラグで指定された値をConfigに適用
	config.ContextSize = *contextSize

	var outWriter io.Writer

	if *outputFile != "" {
		f, err := ctx.FileCreator(*outputFile)
		if err != nil {
			logger.Error("Failed to create output file", "path", *outputFile, "error", err)
			return 1
		}
		defer f.Close()
		outWriter = io.MultiWriter(ctx.Stdout, f)
	} else {
		outWriter = ctx.Stdout
	}

	f, err := ctx.FileReader(config.InputFilePath)
	if err != nil {
		logger.Error("Failed to open input file", "path", config.InputFilePath, "error", err)
		return 1
	}
	defer f.Close()

	// 検索実行時にコンテキストサイズを渡す
	results, err := SearchStream(f, config.Queries, config.ContextSize)
	if err != nil {
		logger.Error("Search failed", "error", err)
		return 1
	}

	WriteResults(outWriter, results, config.Queries)

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
		FileCreator: func(path string) (io.WriteCloser, error) {
			return os.Create(path)
		},
	}

	os.Exit(Run(ctx))
}
