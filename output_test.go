package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintResults(t *testing.T) {
	tests := []struct {
		name           string
		result         *AnalysisResult
		baseDir        string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "empty results",
			result: &AnalysisResult{
				FunctionsWithoutTests: nil,
				MisplacedTests:        nil,
			},
			baseDir: "/test/project",
			wantContains: []string{
				"GO TEST COVERAGE ANALYSIS",
				"/test/project",
				"FUNCTIONS WITHOUT TEST COVERAGE (0)",
				"All functions have test coverage!",
				"MISPLACED TESTS (0)",
				"All tests are in the correct files!",
				"Summary: 0 functions without tests, 0 misplaced tests",
			},
		},
		{
			name: "functions without tests",
			result: &AnalysisResult{
				FunctionsWithoutTests: []FuncInfo{
					{Name: "Foo", File: "foo.go", Line: 10},
					{Name: "Bar", File: "foo.go", Line: 20},
					{Name: "Baz", File: "bar.go", Line: 5},
				},
				MisplacedTests: nil,
			},
			baseDir: "/test/project",
			wantContains: []string{
				"FUNCTIONS WITHOUT TEST COVERAGE (3)",
				"foo.go:",
				"Line 10: Foo",
				"Line 20: Bar",
				"bar.go:",
				"Line 5: Baz",
				"Summary: 3 functions without tests, 0 misplaced tests",
			},
			wantNotContain: []string{
				"All functions have test coverage!",
			},
		},
		{
			name: "method without test",
			result: &AnalysisResult{
				FunctionsWithoutTests: []FuncInfo{
					{Name: "Method", File: "type.go", Line: 15, Receiver: "MyType"},
				},
				MisplacedTests: nil,
			},
			baseDir: "/test/project",
			wantContains: []string{
				"Line 15: (MyType).Method",
			},
		},
		{
			name: "misplaced tests",
			result: &AnalysisResult{
				FunctionsWithoutTests: nil,
				MisplacedTests: []MisplacedTest{
					{
						Test:         TestInfo{Name: "TestFoo", File: "bar_test.go", Line: 10, CalledFuncs: []string{"Foo"}},
						ExpectedFile: "foo_test.go",
						ActualFile:   "bar_test.go",
					},
				},
			},
			baseDir: "/test/project",
			wantContains: []string{
				"MISPLACED TESTS (1)",
				"TestFoo (line 10):",
				"Current file:  bar_test.go",
				"Expected file: foo_test.go",
				"Summary: 0 functions without tests, 1 misplaced tests",
			},
			wantNotContain: []string{
				"All tests are in the correct files!",
			},
		},
		{
			name: "low coverage functions",
			result: &AnalysisResult{
				FunctionsWithoutTests: nil,
				MisplacedTests:        nil,
				LowCoverageFuncs: []LowCoverageFunc{
					{File: "foo.go", Line: 10, Name: "FuncA", Coverage: 50.0, Threshold: 80.0},
					{File: "foo.go", Line: 25, Name: "FuncB", Coverage: 75.5, Threshold: 80.0},
					{File: "bar.go", Line: 5, Name: "FuncC", Coverage: 0.0, Threshold: 80.0},
				},
			},
			baseDir: "/test/project",
			wantContains: []string{
				"LOW COVERAGE FUNCTIONS (below 80.0%) (3)",
				"foo.go:",
				"Line 10: FuncA (50.0%)",
				"Line 25: FuncB (75.5%)",
				"bar.go:",
				"Line 5: FuncC (0.0%)",
				"3 low coverage functions",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			printResults(tt.result, tt.baseDir)

			w.Close()
			os.Stdout = old

			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("Output should contain %q, but it doesn't.\nOutput:\n%s", want, output)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("Output should NOT contain %q, but it does.\nOutput:\n%s", notWant, output)
				}
			}
		})
	}
}
