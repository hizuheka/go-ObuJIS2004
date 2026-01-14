package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestParseArgs は実行ファイル名パースの正常系・異常系を網羅します
func TestParseArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		execPath    string
		wantQueries []string
		wantErr     bool
	}{
		{
			name:        "Normal_TwoQueries",
			args:        []string{"file.txt"},
			execPath:    "/bin/grep_error_warn",
			wantQueries: []string{"error", "warn"},
			wantErr:     false,
		},
		{
			name:     "Error_NoUnderscore",
			args:     []string{"file.txt"},
			execPath: "grep",
			wantErr:  true,
		},
		{
			name:     "Error_NoInputFile",
			args:     []string{},
			execPath: "grep_error",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseArgs(tt.args, tt.execPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got.Queries) != len(tt.wantQueries) {
					t.Errorf("Query count mismatch. got %v, want %v", got.Queries, tt.wantQueries)
				}
			}
		})
	}
}

// TestSearchStream_LazyInit はルーン変換の遅延初期化を含めたロジックを確認します
func TestSearchStream_LazyInit(t *testing.T) {
	// 1行目にヒットなし、2行目にヒットあり（ここで初めて変換が走る）
	content := "no match here\nprefix MATCH suffix"
	r := strings.NewReader(content)

	results, err := SearchStream(r, []string{"MATCH"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if results["MATCH"].Count != 1 {
		t.Errorf("Count should be 1, got %d", results["MATCH"].Count)
	}

	expectedSnippet := "prefix MATCH suffix"
	if results["MATCH"].Snippets[0] != expectedSnippet {
		t.Errorf("Snippet mismatch. got %q, want %q", results["MATCH"].Snippets[0], expectedSnippet)
	}
}

// TestRun_Integration は全体のフローを確認します
func TestRun_Integration(t *testing.T) {
	mockStdout := new(bytes.Buffer)
	// モックReader: 2行の入力
	mockReader := func(_ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("Line 1: error occurred\nLine 2: warning here\n")), nil
	}

	ctx := AppContext{
		Args:       []string{"app", "dummy.log"}, // Args[0]は無視される
		ExecPath:   "app_error_warning",          // ファイル名から error と warning を抽出
		Stdout:     mockStdout,
		Stderr:     io.Discard,
		FileReader: mockReader,
	}

	if code := Run(ctx); code != 0 {
		t.Errorf("Run() exit code = %d", code)
	}

	output := mockStdout.String()
	// 出力チェック
	if !strings.Contains(output, "[error]") || !strings.Contains(output, "該当数: 1") {
		t.Error("Output missing error results")
	}
	if !strings.Contains(output, "[warning]") || !strings.Contains(output, "該当数: 1") {
		t.Error("Output missing warning results")
	}
}
