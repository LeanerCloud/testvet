package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		{
			name: "chained call",
			code: `package test; func f() { a.b.Method() }`,
			want: "Method",
		},
		{
			name: "function literal call",
			code: `package test; func f() { func(){}() }`,
			want: "",
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

func TestShouldSkipDir(t *testing.T) {
	tests := []struct {
		name     string
		dirName  string
		isDir    bool
		expected bool
	}{
		{"vendor dir", "vendor", true, true},
		{"testdata dir", "testdata", true, true},
		{"hidden dir", ".git", true, true},
		{"regular dir", "src", true, false},
		{"file not dir", "vendor", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &mockFileInfo{name: tt.dirName, isDir: tt.isDir}
			got := shouldSkipDir(info)
			if got != tt.expected {
				t.Errorf("shouldSkipDir(%q, isDir=%v) = %v, want %v", tt.dirName, tt.isDir, got, tt.expected)
			}
		})
	}
}

type mockFileInfo struct {
	name  string
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) Sys() interface{}   { return nil }

func TestBuildTestedFuncsMap(t *testing.T) {
	fileTests := map[string][]TestInfo{
		"a_test.go": {
			{Name: "TestA", CalledFuncs: []string{"FuncA", "helper"}},
		},
		"b_test.go": {
			{Name: "TestB", CalledFuncs: []string{"FuncB"}},
		},
	}

	result := buildTestedFuncsMap(fileTests)

	expected := []string{"FuncA", "helper", "FuncB"}
	for _, fn := range expected {
		if !result[fn] {
			t.Errorf("Expected %q to be in tested funcs map", fn)
		}
	}
}

func TestIsFunctionTested(t *testing.T) {
	testedFuncs := map[string]bool{
		"Foo":        true,
		"MyType_Bar": true,
	}

	tests := []struct {
		name     string
		funcInfo FuncInfo
		expected bool
	}{
		{"simple tested", FuncInfo{Name: "Foo"}, true},
		{"method tested", FuncInfo{Name: "Bar", Receiver: "MyType"}, true},
		{"not tested", FuncInfo{Name: "Baz"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFunctionTested(tt.funcInfo, testedFuncs)
			if got != tt.expected {
				t.Errorf("isFunctionTested(%v) = %v, want %v", tt.funcInfo, got, tt.expected)
			}
		})
	}
}

func TestMatchesFunctionCall(t *testing.T) {
	tests := []struct {
		name       string
		funcInfo   FuncInfo
		calledFunc string
		expected   bool
	}{
		{"exact match", FuncInfo{Name: "Foo"}, "Foo", true},
		{"method match", FuncInfo{Name: "Bar", Receiver: "MyType"}, "MyType_Bar", true},
		{"no match", FuncInfo{Name: "Foo"}, "Bar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFunctionCall(tt.funcInfo, tt.calledFunc)
			if got != tt.expected {
				t.Errorf("matchesFunctionCall(%v, %q) = %v, want %v", tt.funcInfo, tt.calledFunc, got, tt.expected)
			}
		})
	}
}

func TestFindFunctionsWithoutTests(t *testing.T) {
	fileFunctions := map[string][]FuncInfo{
		"a.go": {
			{Name: "TestedFunc", File: "a.go", Line: 10},
			{Name: "UntestedFunc", File: "a.go", Line: 20},
		},
	}
	testedFuncs := map[string]bool{"TestedFunc": true}

	result := findFunctionsWithoutTests(fileFunctions, testedFuncs)

	if len(result) != 1 {
		t.Fatalf("Expected 1 untested function, got %d", len(result))
	}
	if result[0].Name != "UntestedFunc" {
		t.Errorf("Expected UntestedFunc, got %s", result[0].Name)
	}
}

func TestFindPrimarySourceFile(t *testing.T) {
	fileFunctions := map[string][]FuncInfo{
		"a.go": {{Name: "FuncA"}, {Name: "FuncA2"}},
		"b.go": {{Name: "FuncB"}},
	}

	tests := []struct {
		name        string
		calledFuncs []string
		expected    string
	}{
		{"single file", []string{"FuncB"}, "b.go"},
		{"multiple from same file", []string{"FuncA", "FuncA2"}, "a.go"},
		{"mixed - a wins", []string{"FuncA", "FuncA2", "FuncB"}, "a.go"},
		{"no matches", []string{"Unknown"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findPrimarySourceFile(tt.calledFuncs, fileFunctions)
			if got != tt.expected {
				t.Errorf("findPrimarySourceFile(%v) = %q, want %q", tt.calledFuncs, got, tt.expected)
			}
		})
	}
}

func TestCheckTestPlacement(t *testing.T) {
	fileFunctions := map[string][]FuncInfo{
		"a.go": {{Name: "FuncA"}},
		"b.go": {{Name: "FuncB"}},
	}

	tests := []struct {
		name           string
		test           TestInfo
		testFile       string
		expectMisplace bool
	}{
		{
			"correctly placed",
			TestInfo{Name: "TestA", CalledFuncs: []string{"FuncA"}},
			"a_test.go",
			false,
		},
		{
			"misplaced",
			TestInfo{Name: "TestB", CalledFuncs: []string{"FuncB"}},
			"a_test.go",
			true,
		},
		{
			"no called funcs",
			TestInfo{Name: "TestEmpty", CalledFuncs: nil},
			"a_test.go",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkTestPlacement(tt.test, tt.testFile, fileFunctions)
			if tt.expectMisplace && result == nil {
				t.Error("Expected misplaced test, got nil")
			}
			if !tt.expectMisplace && result != nil {
				t.Errorf("Expected no misplacement, got %v", result)
			}
		})
	}
}

func TestFindMisplacedTests(t *testing.T) {
	fileTests := map[string][]TestInfo{
		"a_test.go": {
			{Name: "TestA", CalledFuncs: []string{"FuncA"}},
			{Name: "TestB", CalledFuncs: []string{"FuncB"}, Line: 10},
		},
	}
	fileFunctions := map[string][]FuncInfo{
		"a.go": {{Name: "FuncA"}},
		"b.go": {{Name: "FuncB"}},
	}

	result := findMisplacedTests(fileTests, fileFunctions)

	if len(result) != 1 {
		t.Fatalf("Expected 1 misplaced test, got %d", len(result))
	}
	if result[0].Test.Name != "TestB" {
		t.Errorf("Expected TestB to be misplaced, got %s", result[0].Test.Name)
	}
}

func TestParseProjectFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-parse-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sourceContent := `package testpkg
func Foo() {}
`
	err = os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte(sourceContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	testContent := `package testpkg
import "testing"
func TestFoo(t *testing.T) { Foo() }
`
	err = os.WriteFile(filepath.Join(tmpDir, "source_test.go"), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	result, err := parseProjectFiles(tmpDir, false, false)
	if err != nil {
		t.Fatalf("parseProjectFiles failed: %v", err)
	}

	if len(result.fileFunctions) != 1 {
		t.Errorf("Expected 1 source file, got %d", len(result.fileFunctions))
	}
	if len(result.fileTests) != 1 {
		t.Errorf("Expected 1 test file, got %d", len(result.fileTests))
	}
}

func TestProcessFileDeclarations(t *testing.T) {
	code := `package testpkg
func Foo() {}
func init() {}
func main() {}
type Bar struct{}
func (b *Bar) Method() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	fileFunctions := make(map[string][]FuncInfo)
	fileTests := make(map[string][]TestInfo)

	processFileDeclarations(file, fset, "test.go", false, false, fileFunctions, fileTests)

	funcs := fileFunctions["test.go"]
	if len(funcs) != 2 { // Foo and Bar.Method (init and main excluded)
		t.Errorf("Expected 2 functions, got %d", len(funcs))
		for _, f := range funcs {
			t.Logf("  - %s (receiver: %s)", f.Name, f.Receiver)
		}
	}
}

func TestBuildFuncInfo(t *testing.T) {
	code := `package test
type MyType struct{}
func (m *MyType) Method() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			info := buildFuncInfo(funcDecl, funcDecl.Name.Name, "test.go", 3)
			if info.Name != "Method" {
				t.Errorf("Expected name Method, got %s", info.Name)
			}
			if info.Receiver != "MyType" {
				t.Errorf("Expected receiver MyType, got %s", info.Receiver)
			}
		}
	}
}

func TestParseProjectFiles_InvalidGoFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-invalid-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an invalid Go file
	invalidContent := `package testpkg
func Broken( {
`
	err = os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	// Should not fail, just skip the invalid file
	result, err := parseProjectFiles(tmpDir, false, false)
	if err != nil {
		t.Fatalf("parseProjectFiles should not fail on invalid file: %v", err)
	}

	// No functions should be found from the invalid file
	if len(result.fileFunctions) != 0 {
		t.Errorf("Expected 0 source files from invalid Go, got %d", len(result.fileFunctions))
	}
}

func TestParseProjectFiles_VerboseMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-verbose-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create an invalid Go file
	invalidContent := `package testpkg
func Broken( {
`
	err = os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	// Run with verbose=true (warning should be printed to stderr)
	result, err := parseProjectFiles(tmpDir, false, true)
	if err != nil {
		t.Fatalf("parseProjectFiles should not fail: %v", err)
	}

	if len(result.fileFunctions) != 0 {
		t.Errorf("Expected 0 source files, got %d", len(result.fileFunctions))
	}
}

func TestFindMisplacedTests_Sorting(t *testing.T) {
	// Multiple misplaced tests should be sorted by file, then line
	fileTests := map[string][]TestInfo{
		"z_test.go": {
			{Name: "TestZ1", CalledFuncs: []string{"FuncB"}, Line: 20},
			{Name: "TestZ2", CalledFuncs: []string{"FuncB"}, Line: 10},
		},
		"a_test.go": {
			{Name: "TestA", CalledFuncs: []string{"FuncB"}, Line: 5},
		},
	}
	fileFunctions := map[string][]FuncInfo{
		"b.go": {{Name: "FuncB"}},
	}

	result := findMisplacedTests(fileTests, fileFunctions)

	if len(result) != 3 {
		t.Fatalf("Expected 3 misplaced tests, got %d", len(result))
	}

	// Should be sorted: a_test.go:5, z_test.go:10, z_test.go:20
	expectedOrder := []struct {
		file string
		line int
	}{
		{"a_test.go", 5},
		{"z_test.go", 10},
		{"z_test.go", 20},
	}

	for i, expected := range expectedOrder {
		if result[i].ActualFile != expected.file || result[i].Test.Line != expected.line {
			t.Errorf("Position %d: expected %s:%d, got %s:%d",
				i, expected.file, expected.line, result[i].ActualFile, result[i].Test.Line)
		}
	}
}

func TestGetReceiverType_UnknownExpr(t *testing.T) {
	// Test with an expression type that doesn't match any case
	// Using a BasicLit as an example of an unexpected type
	expr := &ast.BasicLit{Kind: token.INT, Value: "42"}
	result := getReceiverType(expr)
	if result != "" {
		t.Errorf("Expected empty string for unknown expr type, got %q", result)
	}
}

func TestFindFunctionsWithoutTests_Sorting(t *testing.T) {
	// Test that results are sorted by file, then line
	fileFunctions := map[string][]FuncInfo{
		"z.go": {
			{Name: "FuncZ2", File: "z.go", Line: 30},
			{Name: "FuncZ1", File: "z.go", Line: 10},
		},
		"a.go": {
			{Name: "FuncA", File: "a.go", Line: 20},
		},
	}
	testedFuncs := map[string]bool{} // none tested

	result := findFunctionsWithoutTests(fileFunctions, testedFuncs)

	if len(result) != 3 {
		t.Fatalf("Expected 3 untested functions, got %d", len(result))
	}

	// Should be sorted: a.go:20, z.go:10, z.go:30
	expectedOrder := []struct {
		file string
		line int
	}{
		{"a.go", 20},
		{"z.go", 10},
		{"z.go", 30},
	}

	for i, expected := range expectedOrder {
		if result[i].File != expected.file || result[i].Line != expected.line {
			t.Errorf("Position %d: expected %s:%d, got %s:%d",
				i, expected.file, expected.line, result[i].File, result[i].Line)
		}
	}
}

func TestExtractFunctionNameFromTest(t *testing.T) {
	tests := []struct {
		testName string
		expected string
	}{
		{"TestFoo", "Foo"},
		{"TestFooBar", "FooBar"},
		{"TestFoo_SubTest", "Foo"},
		{"Test_Foo", "Foo"},
		{"Test_Foo_Bar", "Foo"},
		{"TestNeedReplaceOnDemandInstances", "NeedReplaceOnDemandInstances"},
		{"BenchmarkFoo", ""},  // Not a Test
		{"NotATest", ""},
		{"Test", ""},  // Just "Test" with nothing after
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			got := extractFunctionNameFromTest(tt.testName)
			if got != tt.expected {
				t.Errorf("extractFunctionNameFromTest(%q) = %q, want %q", tt.testName, got, tt.expected)
			}
		})
	}
}

func TestFindSourceByTestName(t *testing.T) {
	fileFunctions := map[string][]FuncInfo{
		"asg_capacity.go":    {{Name: "needReplaceOnDemandInstances"}},
		"instance_manager.go": {{Name: "makeInstancesWithCatalog"}, {Name: "CreateInstance"}},
	}

	tests := []struct {
		name         string
		testName     string
		calledFuncs  []string
		expectedFile string
	}{
		{
			name:         "matches function under test by naming convention",
			testName:     "TestNeedReplaceOnDemandInstances",
			calledFuncs:  []string{"needReplaceOnDemandInstances", "makeInstancesWithCatalog", "makeInstancesWithCatalog", "makeInstancesWithCatalog"},
			expectedFile: "asg_capacity.go",
		},
		{
			name:         "case insensitive first letter",
			testName:     "TestCreateInstance",
			calledFuncs:  []string{"CreateInstance"},
			expectedFile: "instance_manager.go",
		},
		{
			name:         "no match - function not called",
			testName:     "TestSomethingElse",
			calledFuncs:  []string{"makeInstancesWithCatalog"},
			expectedFile: "",
		},
		{
			name:         "no match - function not in source files",
			testName:     "TestUnknown",
			calledFuncs:  []string{"Unknown"},
			expectedFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSourceByTestName(tt.testName, tt.calledFuncs, fileFunctions)
			if got != tt.expectedFile {
				t.Errorf("findSourceByTestName(%q) = %q, want %q", tt.testName, got, tt.expectedFile)
			}
		})
	}
}

func TestCheckTestPlacement_NamingConvention(t *testing.T) {
	// This test verifies that naming convention takes precedence over call counting
	fileFunctions := map[string][]FuncInfo{
		"asg_capacity.go":    {{Name: "needReplaceOnDemandInstances"}},
		"instance_manager.go": {{Name: "makeInstancesWithCatalog"}},
	}

	// Test calls needReplaceOnDemandInstances once but makeInstancesWithCatalog 3 times
	// Without naming convention, it would suggest instance_manager_test.go
	// With naming convention, it correctly identifies asg_capacity_test.go
	test := TestInfo{
		Name:        "TestNeedReplaceOnDemandInstances",
		CalledFuncs: []string{"needReplaceOnDemandInstances", "makeInstancesWithCatalog", "makeInstancesWithCatalog", "makeInstancesWithCatalog"},
	}

	result := checkTestPlacement(test, "asg_capacity_test.go", fileFunctions)

	// Should NOT be misplaced - naming convention should match it to asg_capacity.go
	if result != nil {
		t.Errorf("Test should not be misplaced, but got suggestion to move to %s", result.ExpectedFile)
	}
}
