package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// parseResult holds the intermediate result of parsing project files
type parseResult struct {
	fileFunctions map[string][]FuncInfo
	fileTests     map[string][]TestInfo
}

func analyzeProject(dir string, excludePrivate, verbose bool) (*AnalysisResult, error) {
	parsed, err := parseProjectFiles(dir, excludePrivate, verbose)
	if err != nil {
		return nil, err
	}

	testedFuncs := buildTestedFuncsMap(parsed.fileTests)
	functionsWithoutTests := findFunctionsWithoutTests(parsed.fileFunctions, testedFuncs)
	misplacedTests := findMisplacedTests(parsed.fileTests, parsed.fileFunctions)

	return &AnalysisResult{
		FunctionsWithoutTests: functionsWithoutTests,
		MisplacedTests:        misplacedTests,
	}, nil
}

// parseProjectFiles walks the directory and parses all Go files
func parseProjectFiles(dir string, excludePrivate, verbose bool) (*parseResult, error) {
	fileFunctions := make(map[string][]FuncInfo)
	fileTests := make(map[string][]TestInfo)
	fset := token.NewFileSet()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if shouldSkipDir(info) {
			return filepath.SkipDir
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, _ := filepath.Rel(dir, path)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v\n", relPath, err)
			}
			return nil
		}

		isTestFile := strings.HasSuffix(path, "_test.go")
		processFileDeclarations(file, fset, relPath, isTestFile, excludePrivate, fileFunctions, fileTests)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &parseResult{
		fileFunctions: fileFunctions,
		fileTests:     fileTests,
	}, nil
}

// shouldSkipDir returns true if the directory should be skipped
func shouldSkipDir(info os.FileInfo) bool {
	if !info.IsDir() {
		return false
	}
	name := info.Name()
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")
}

// processFileDeclarations extracts function and test declarations from a parsed file
func processFileDeclarations(file *ast.File, fset *token.FileSet, relPath string, isTestFile, excludePrivate bool, fileFunctions map[string][]FuncInfo, fileTests map[string][]TestInfo) {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		funcName := funcDecl.Name.Name
		pos := fset.Position(funcDecl.Pos())

		if isTestFile {
			if isTestFunction(funcName) {
				testInfo := TestInfo{
					Name:        funcName,
					File:        relPath,
					Line:        pos.Line,
					CalledFuncs: extractCalledFunctions(funcDecl),
				}
				fileTests[relPath] = append(fileTests[relPath], testInfo)
			}
		} else {
			if funcName == "init" || funcName == "main" {
				continue
			}
			if excludePrivate && !ast.IsExported(funcName) {
				continue
			}

			funcInfo := buildFuncInfo(funcDecl, funcName, relPath, pos.Line)
			fileFunctions[relPath] = append(fileFunctions[relPath], funcInfo)
		}
	}
}

// buildFuncInfo creates a FuncInfo from a function declaration
func buildFuncInfo(funcDecl *ast.FuncDecl, funcName, relPath string, line int) FuncInfo {
	var receiver string
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		receiver = getReceiverType(funcDecl.Recv.List[0].Type)
	}
	return FuncInfo{
		Name:     funcName,
		File:     relPath,
		Line:     line,
		Receiver: receiver,
	}
}

// buildTestedFuncsMap creates a set of function names that are called from tests
func buildTestedFuncsMap(fileTests map[string][]TestInfo) map[string]bool {
	testedFuncs := make(map[string]bool)
	for _, tests := range fileTests {
		for _, test := range tests {
			for _, calledFunc := range test.CalledFuncs {
				testedFuncs[calledFunc] = true
			}
		}
	}
	return testedFuncs
}

// findFunctionsWithoutTests returns functions that are not in the tested set
func findFunctionsWithoutTests(fileFunctions map[string][]FuncInfo, testedFuncs map[string]bool) []FuncInfo {
	var result []FuncInfo
	for _, funcs := range fileFunctions {
		for _, f := range funcs {
			if !isFunctionTested(f, testedFuncs) {
				result = append(result, f)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		return result[i].Line < result[j].Line
	})

	return result
}

// isFunctionTested checks if a function is in the tested set
func isFunctionTested(f FuncInfo, testedFuncs map[string]bool) bool {
	key := f.Name
	if f.Receiver != "" {
		key = f.Receiver + "_" + f.Name
	}
	return testedFuncs[key] || testedFuncs[f.Name]
}

// findMisplacedTests finds tests that are in the wrong file
func findMisplacedTests(fileTests map[string][]TestInfo, fileFunctions map[string][]FuncInfo) []MisplacedTest {
	var result []MisplacedTest

	for testFile, tests := range fileTests {
		for _, test := range tests {
			if misplaced := checkTestPlacement(test, testFile, fileFunctions); misplaced != nil {
				result = append(result, *misplaced)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ActualFile != result[j].ActualFile {
			return result[i].ActualFile < result[j].ActualFile
		}
		return result[i].Test.Line < result[j].Test.Line
	})

	return result
}

// checkTestPlacement checks if a test is in the correct file
func checkTestPlacement(test TestInfo, testFile string, fileFunctions map[string][]FuncInfo) *MisplacedTest {
	if len(test.CalledFuncs) == 0 {
		return nil
	}

	primarySource := findPrimarySourceFile(test.CalledFuncs, fileFunctions)
	if primarySource == "" {
		return nil
	}

	expectedTestFile := strings.TrimSuffix(primarySource, ".go") + "_test.go"
	if testFile == expectedTestFile {
		return nil
	}

	// Only report if in the same directory
	if filepath.Dir(testFile) != filepath.Dir(expectedTestFile) {
		return nil
	}

	return &MisplacedTest{
		Test:         test,
		ExpectedFile: expectedTestFile,
		ActualFile:   testFile,
	}
}

// findPrimarySourceFile finds the source file with the most called functions
func findPrimarySourceFile(calledFuncs []string, fileFunctions map[string][]FuncInfo) string {
	sourceFileCounts := make(map[string]int)

	for _, calledFunc := range calledFuncs {
		for sourceFile, funcs := range fileFunctions {
			for _, f := range funcs {
				if matchesFunctionCall(f, calledFunc) {
					sourceFileCounts[sourceFile]++
					break
				}
			}
		}
	}

	var primarySource string
	maxCalls := 0
	for src, count := range sourceFileCounts {
		if count > maxCalls {
			maxCalls = count
			primarySource = src
		}
	}

	return primarySource
}

// matchesFunctionCall checks if a function matches a called function name
func matchesFunctionCall(f FuncInfo, calledFunc string) bool {
	funcKey := f.Name
	if f.Receiver != "" {
		funcKey = f.Receiver + "_" + f.Name
	}
	return funcKey == calledFunc || f.Name == calledFunc
}

// isTestFunction checks if a function name is a test function
func isTestFunction(name string) bool {
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Example") ||
		strings.HasPrefix(name, "Fuzz")
}

// getReceiverType extracts the type name from a receiver expression
func getReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return getReceiverType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		// Generic type like T[V]
		return getReceiverType(t.X)
	case *ast.IndexListExpr:
		// Generic type with multiple params like T[K, V]
		return getReceiverType(t.X)
	}
	return ""
}

// extractCalledFunctions walks the AST of a function and extracts all function calls
func extractCalledFunctions(funcDecl *ast.FuncDecl) []string {
	if funcDecl.Body == nil {
		return nil
	}

	seen := make(map[string]bool)
	var calledFuncs []string

	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			funcName := extractFuncNameFromCall(node)
			if funcName != "" && !seen[funcName] {
				seen[funcName] = true
				calledFuncs = append(calledFuncs, funcName)
			}
		}
		return true
	})

	return calledFuncs
}

// extractFuncNameFromCall extracts the function name from a call expression
func extractFuncNameFromCall(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// Direct function call: foo()
		return fn.Name
	case *ast.SelectorExpr:
		// Method call or package function: obj.Method() or pkg.Func()
		if ident, ok := fn.X.(*ast.Ident); ok {
			// Could be receiver.Method() or package.Func()
			// Return as Type_Method format for method matching
			return ident.Name + "_" + fn.Sel.Name
		}
		// For chained calls like a.b.Method(), just return the method name
		return fn.Sel.Name
	}
	return ""
}
