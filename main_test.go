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
				// デフォルト値が入っていることの確認
				if got.ContextSize != DefaultContextSize {
					t.Errorf("ContextSize default mismatch. got %d, want %d", got.ContextSize, DefaultContextSize)
				}
			}
		})
	}
}

// TestSearchStream_ContextSize は指定された文字数で切り出されるか確認します
func TestSearchStream_ContextSize(t *testing.T) {
	// "TARGET" の前後に数字を配置
	content := "12345678901234567890TARGET12345678901234567890"
	//          ^^^^^^^^^^^^^^^^^^^^      ^^^^^^^^^^^^^^^^^^^^
	//          20 chars                  20 chars

	r := strings.NewReader(content)

	// ケース1: デフォルトの20文字
	results, err := SearchStream(r, []string{"TARGET"}, 20)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	want20 := "12345678901234567890TARGET12345678901234567890"
	if results["TARGET"].Snippets[0] != want20 {
		t.Errorf("Context(20) mismatch.\n got:  %q\n want: %q", results["TARGET"].Snippets[0], want20)
	}

	// ケース2: 5文字指定（Readerをリセット）
	r.Reset(content)
	results, err = SearchStream(r, []string{"TARGET"}, 5)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	want5 := "67890TARGET12345"
	if results["TARGET"].Snippets[0] != want5 {
		t.Errorf("Context(5) mismatch.\n got:  %q\n want: %q", results["TARGET"].Snippets[0], want5)
	}
}

// TestRun_Integration_FlagCheck は -n フラグの動作を確認します
func TestRun_Integration_FlagCheck(t *testing.T) {
	mockStdout := new(bytes.Buffer)
	mockReader := func(_ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("PRE_TEXT_TARGET_POST_TEXT")), nil
	}

	// -n 4 を指定して実行
	ctx := AppContext{
		Args:        []string{"app", "-n", "4", "dummy.log"}, // Args[0]無視, -n 4 指定
		ExecPath:    "app_TARGET",
		Stdout:      mockStdout,
		Stderr:      io.Discard,
		FileReader:  mockReader,
		FileCreator: func(_ string) (io.WriteCloser, error) { return nil, nil },
	}

	if code := Run(ctx); code != 0 {
		t.Errorf("Run() exit code = %d", code)
	}

	output := mockStdout.String()

	// 修正済み: 入力 "PRE_TEXT_TARGET_POST_TEXT" に対する前後4文字の正しい期待値
	// 前4文字: "EXT_" (E, X, T, _)
	// 後4文字: "_POS" (_, P, O, S)
	want := "EXT_TARGET_POS"

	if !strings.Contains(output, want) {
		t.Errorf("Output should contain snippet with 4 chars context.\n Output: %s\n Want partial: %s", output, want)
	}
}
