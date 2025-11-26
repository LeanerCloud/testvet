package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCoverageOutput(t *testing.T) {
	output := `github.com/example/pkg/file.go:20:	FuncA		85.7%
github.com/example/pkg/file.go:35:	FuncB		50.0%
github.com/example/pkg/other.go:10:	FuncC		100.0%
total:					(statements)	78.5%`

	tests := []struct {
		name          string
		threshold     float64
		expectedCount int
	}{
		{"threshold 90", 90, 2},  // FuncA (85.7%) and FuncB (50.0%)
		{"threshold 60", 60, 1},  // Only FuncB (50.0%)
		{"threshold 50", 50, 0},  // None below 50
		{"threshold 100", 100, 2}, // FuncA and FuncB (FuncC is exactly 100)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCoverageOutput(output, "/tmp", tt.threshold)
			if err != nil {
				t.Fatalf("parseCoverageOutput failed: %v", err)
			}
			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d functions below threshold %.1f, got %d",
					tt.expectedCount, tt.threshold, len(result))
				for _, f := range result {
					t.Logf("  - %s: %.1f%%", f.Name, f.Coverage)
				}
			}
		})
	}
}

func TestParseCoverageOutput_Fields(t *testing.T) {
	output := `github.com/example/pkg/file.go:25:	MyFunc		75.5%
total:					(statements)	75.5%`

	result, err := parseCoverageOutput(output, "/tmp", 80)
	if err != nil {
		t.Fatalf("parseCoverageOutput failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(result))
	}

	f := result[0]
	if f.Name != "MyFunc" {
		t.Errorf("Expected Name 'MyFunc', got %q", f.Name)
	}
	if f.Line != 25 {
		t.Errorf("Expected Line 25, got %d", f.Line)
	}
	if f.Coverage != 75.5 {
		t.Errorf("Expected Coverage 75.5, got %.1f", f.Coverage)
	}
	if f.Threshold != 80 {
		t.Errorf("Expected Threshold 80, got %.1f", f.Threshold)
	}
}

func TestParseCoverageOutput_Sorting(t *testing.T) {
	output := `github.com/pkg/b.go:20:	FuncB		50.0%
github.com/pkg/a.go:30:	FuncA2		40.0%
github.com/pkg/a.go:10:	FuncA1		30.0%
total:					(statements)	40.0%`

	result, err := parseCoverageOutput(output, "/tmp", 100)
	if err != nil {
		t.Fatalf("parseCoverageOutput failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}

	// Should be sorted by file, then line
	// a.go:10 (FuncA1), a.go:30 (FuncA2), b.go:20 (FuncB)
	expectedOrder := []string{"FuncA1", "FuncA2", "FuncB"}
	for i, expected := range expectedOrder {
		if result[i].Name != expected {
			t.Errorf("Position %d: expected %s, got %s", i, expected, result[i].Name)
		}
	}
}

func TestAnalyzeCoverage_Integration(t *testing.T) {
	// Create a temporary Go project
	tmpDir, err := os.MkdirTemp("", "test-coverage-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module testpkg

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Create source file
	sourceContent := `package testpkg

func TestedFunc() int {
	return 42
}

func PartiallyTested(x int) int {
	if x > 0 {
		return x
	}
	return -x
}

func UntestedFunc() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Create test file that only tests TestedFunc and partially tests PartiallyTested
	testContent := `package testpkg

import "testing"

func TestTestedFunc(t *testing.T) {
	if TestedFunc() != 42 {
		t.Error("unexpected result")
	}
}

func TestPartiallyTested(t *testing.T) {
	if PartiallyTested(5) != 5 {
		t.Error("unexpected result")
	}
	// Note: negative path not tested
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run coverage analysis with threshold 80
	result, err := analyzeCoverage(tmpDir, 80, false)
	if err != nil {
		t.Fatalf("analyzeCoverage failed: %v", err)
	}

	// Should find PartiallyTested (66.7%) and UntestedFunc (0%)
	if len(result) < 1 {
		t.Errorf("Expected at least 1 low coverage function, got %d", len(result))
	}

	// Check that UntestedFunc is in the results with 0%
	foundUntested := false
	for _, f := range result {
		if f.Name == "UntestedFunc" {
			foundUntested = true
			if f.Coverage != 0 {
				t.Errorf("Expected UntestedFunc coverage 0%%, got %.1f%%", f.Coverage)
			}
		}
	}
	if !foundUntested {
		t.Error("Expected to find UntestedFunc in low coverage results")
	}
}

func TestAnalyzeCoverage_TestsFail(t *testing.T) {
	// Create a project where tests fail
	tmpDir, err := os.MkdirTemp("", "test-failing-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	goMod := `module testpkg

go 1.21
`
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Create source file
	sourceContent := `package testpkg

func Foo() int { return 42 }
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Create a failing test
	testContent := `package testpkg

import "testing"

func TestFoo(t *testing.T) {
	if Foo() != 999 { // This will fail
		t.Error("intentional failure")
	}
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// analyzeCoverage should return an error when tests fail
	_, err = analyzeCoverage(tmpDir, 80, false)
	if err == nil {
		t.Error("Expected error when tests fail, got nil")
	}
}

func TestAnalyzeCoverage_NoGoModule(t *testing.T) {
	// Create a directory without go.mod (will fail go test)
	tmpDir, err := os.MkdirTemp("", "test-nomod-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create just a Go file, no go.mod
	sourceContent := `package testpkg

func Foo() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// analyzeCoverage should return an error
	_, err = analyzeCoverage(tmpDir, 80, false)
	if err == nil {
		t.Error("Expected error for directory without go.mod, got nil")
	}
}
