package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestIsTestFunction(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
		want     bool
	}{
		{"Test prefix", "TestFoo", true},
		{"Benchmark prefix", "BenchmarkBar", true},
		{"Example prefix", "ExampleBaz", true},
		{"Fuzz prefix", "FuzzQux", true},
		{"Regular function", "DoSomething", false},
		{"Test in middle", "DoTestSomething", false},
		{"Lowercase test", "testFoo", false},
		{"Empty string", "", false},
		{"Just Test", "Test", true},
		{"Test with underscore", "Test_something", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFunction(tt.funcName)
			if got != tt.want {
				t.Errorf("isTestFunction(%q) = %v, want %v", tt.funcName, got, tt.want)
			}
		})
	}
}

func TestGetReceiverType(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{
			"Simple ident",
			&ast.Ident{Name: "MyType"},
			"MyType",
		},
		{
			"Pointer receiver",
			&ast.StarExpr{X: &ast.Ident{Name: "MyType"}},
			"MyType",
		},
		{
			"Generic type",
			&ast.IndexExpr{X: &ast.Ident{Name: "Container"}},
			"Container",
		},
		{
			"Generic with multiple params",
			&ast.IndexListExpr{X: &ast.Ident{Name: "Map"}},
			"Map",
		},
		{
			"Nested pointer to generic",
			&ast.StarExpr{X: &ast.IndexExpr{X: &ast.Ident{Name: "List"}}},
			"List",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getReceiverType(tt.expr)
			if got != tt.want {
				t.Errorf("getReceiverType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractCalledFunctions(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantFunc string
		contains []string
	}{
		{
			name: "direct function call",
			code: `package test
func TestFoo(t *testing.T) {
	bar()
	baz()
}`,
			wantFunc: "TestFoo",
			contains: []string{"bar", "baz"},
		},
		{
			name: "method call",
			code: `package test
func TestMethod(t *testing.T) {
	obj.Method()
}`,
			wantFunc: "TestMethod",
			contains: []string{"obj_Method"},
		},
		{
			name: "mixed calls",
			code: `package test
func TestMixed(t *testing.T) {
	foo()
	obj.Bar()
	pkg.Func()
}`,
			wantFunc: "TestMixed",
			contains: []string{"foo", "obj_Bar", "pkg_Func"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("Failed to parse code: %v", err)
			}

			// Find the test function
			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok || funcDecl.Name.Name != tt.wantFunc {
					continue
				}

				got := extractCalledFunctions(funcDecl)
				for _, want := range tt.contains {
					found := false
					for _, g := range got {
						if g == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("extractCalledFunctions() missing %q, got %v", want, got)
					}
				}
			}
		})
	}
}

func TestExtractFuncNameFromCall(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "simple function",
			code: `package test; func f() { foo() }`,
			want: "foo",
		},
		{
			name: "method call",
			code: `package test; func f() { obj.Method() }`,
			want: "obj_Method",
		},
		{
			name: "package function",
			code: `package test; func f() { fmt.Println() }`,
			want: "fmt_Println",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("Failed to parse code: %v", err)
			}

			// Find the call expression
			var callExpr *ast.CallExpr
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					callExpr = call
					return false
				}
				return true
			})

			if callExpr == nil {
				t.Fatal("No call expression found")
			}

			got := extractFuncNameFromCall(callExpr)
			if got != tt.want {
				t.Errorf("extractFuncNameFromCall() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnalyzeProject_IndirectTesting(t *testing.T) {
	// Test that functions called within tests are marked as tested
	tmpDir, err := os.MkdirTemp("", "test-indirect-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create source file with helper function
	sourceContent := `package testpkg

func PublicFunc() {
	helperFunc()
}

func helperFunc() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Create test that calls helperFunc directly (indirect testing)
	testContent := `package testpkg

import "testing"

func TestPublicFunc(t *testing.T) {
	PublicFunc()
	helperFunc() // Also testing helper directly
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result, err := analyzeProject(tmpDir, false, false)
	if err != nil {
		t.Fatalf("analyzeProject failed: %v", err)
	}

	// Both PublicFunc and helperFunc should be considered tested
	if len(result.FunctionsWithoutTests) != 0 {
		t.Errorf("Expected 0 functions without tests (indirect testing), got %d:", len(result.FunctionsWithoutTests))
		for _, f := range result.FunctionsWithoutTests {
			t.Logf("  - %s", f.Name)
		}
	}
}

func TestAnalyzeProject(t *testing.T) {
	// Create a temporary directory with test Go files
	tmpDir, err := os.MkdirTemp("", "test-analyzer-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a source file with functions
	sourceContent := `package testpkg

func PublicFunc() {}

func privateFunc() {}

type MyType struct{}

func (m *MyType) Method() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// Create a test file that calls PublicFunc
	testContent := `package testpkg

import "testing"

func TestPublicFunc(t *testing.T) {
	PublicFunc()
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	t.Run("finds functions without tests", func(t *testing.T) {
		result, err := analyzeProject(tmpDir, false, false)
		if err != nil {
			t.Fatalf("analyzeProject failed: %v", err)
		}

		// Should find privateFunc and MyType.Method as untested (PublicFunc is called)
		if len(result.FunctionsWithoutTests) != 2 {
			t.Errorf("Expected 2 functions without tests, got %d", len(result.FunctionsWithoutTests))
			for _, f := range result.FunctionsWithoutTests {
				t.Logf("  - %s.%s", f.Receiver, f.Name)
			}
		}
	})

	t.Run("excludes private functions when flag set", func(t *testing.T) {
		result, err := analyzeProject(tmpDir, true, false)
		if err != nil {
			t.Fatalf("analyzeProject failed: %v", err)
		}

		// Should only find MyType.Method as untested (private func excluded, PublicFunc is called)
		if len(result.FunctionsWithoutTests) != 1 {
			t.Errorf("Expected 1 function without tests (excluding private), got %d", len(result.FunctionsWithoutTests))
		}
	})
}

func TestAnalyzeProject_MisplacedTests(t *testing.T) {
	// Create a temporary directory with test Go files
	tmpDir, err := os.MkdirTemp("", "test-misplaced-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create file a.go with FuncA
	aContent := `package testpkg

func FuncA() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(aContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write a.go: %v", err)
	}

	// Create file b.go with FuncB
	bContent := `package testpkg

func FuncB() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(bContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write b.go: %v", err)
	}

	// Create test file a_test.go but put TestFuncB in it (misplaced!)
	// TestFuncB calls FuncB which is in b.go, so it should be in b_test.go
	testContent := `package testpkg

import "testing"

func TestFuncA(t *testing.T) {
	FuncA()
}
func TestFuncB(t *testing.T) {
	FuncB()
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "a_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result, err := analyzeProject(tmpDir, false, false)
	if err != nil {
		t.Fatalf("analyzeProject failed: %v", err)
	}

	// Should detect TestFuncB is misplaced (calls FuncB from b.go, should be in b_test.go)
	if len(result.MisplacedTests) != 1 {
		t.Errorf("Expected 1 misplaced test, got %d", len(result.MisplacedTests))
	}

	if len(result.MisplacedTests) > 0 {
		mt := result.MisplacedTests[0]
		if mt.Test.Name != "TestFuncB" {
			t.Errorf("Expected misplaced test to be TestFuncB, got %s", mt.Test.Name)
		}
		if mt.ExpectedFile != "b_test.go" {
			t.Errorf("Expected file should be b_test.go, got %s", mt.ExpectedFile)
		}
	}
}

func TestAnalyzeProject_SkipsVendor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-vendor-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create vendor directory with a Go file
	vendorDir := filepath.Join(tmpDir, "vendor")
	err = os.MkdirAll(vendorDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create vendor dir: %v", err)
	}

	vendorContent := `package vendorpkg

func VendorFunc() {}
`
	err = os.WriteFile(filepath.Join(vendorDir, "vendor.go"), []byte(vendorContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write vendor file: %v", err)
	}

	// Create a regular source file
	sourceContent := `package testpkg

func RegularFunc() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	result, err := analyzeProject(tmpDir, false, false)
	if err != nil {
		t.Fatalf("analyzeProject failed: %v", err)
	}

	// Should only find RegularFunc, not VendorFunc
	if len(result.FunctionsWithoutTests) != 1 {
		t.Errorf("Expected 1 function (vendor excluded), got %d", len(result.FunctionsWithoutTests))
	}

	for _, f := range result.FunctionsWithoutTests {
		if f.Name == "VendorFunc" {
			t.Errorf("VendorFunc should have been excluded from analysis")
		}
	}
}
