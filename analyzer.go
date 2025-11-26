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

func analyzeProject(dir string, excludePrivate, verbose bool) (*AnalysisResult, error) {
	// Maps to hold our data
	// Key: "package/filename.go" -> []FuncInfo
	fileFunctions := make(map[string][]FuncInfo)
	// Key: "package/filename_test.go" -> []TestInfo
	fileTests := make(map[string][]TestInfo)
	// All functions indexed by name for matching
	allFunctions := make(map[string][]FuncInfo) // name -> functions (can have same name in different files)

	fset := token.NewFileSet()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor and hidden directories
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Get relative path for cleaner output
		relPath, _ := filepath.Rel(dir, path)

		// Parse the file
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v\n", relPath, err)
			}
			return nil
		}

		isTestFile := strings.HasSuffix(path, "_test.go")

		// Extract functions
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			funcName := funcDecl.Name.Name
			pos := fset.Position(funcDecl.Pos())

			if isTestFile {
				// Check if it's a test function
				if isTestFunction(funcName) {
					calledFuncs := extractCalledFunctions(funcDecl)
					testInfo := TestInfo{
						Name:        funcName,
						File:        relPath,
						Line:        pos.Line,
						CalledFuncs: calledFuncs,
					}
					fileTests[relPath] = append(fileTests[relPath], testInfo)
				}
			} else {
				// Skip init and main functions
				if funcName == "init" || funcName == "main" {
					continue
				}

				// Skip private functions if requested
				if excludePrivate && !ast.IsExported(funcName) {
					continue
				}

				// Get receiver type if it's a method
				var receiver string
				if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
					receiver = getReceiverType(funcDecl.Recv.List[0].Type)
				}

				funcInfo := FuncInfo{
					Name:     funcName,
					File:     relPath,
					Line:     pos.Line,
					Receiver: receiver,
				}
				fileFunctions[relPath] = append(fileFunctions[relPath], funcInfo)

				// Index by function name for test matching
				key := funcName
				if receiver != "" {
					key = receiver + "." + funcName
				}
				allFunctions[key] = append(allFunctions[key], funcInfo)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Analyze: find functions without tests
	// Mark all functions called within tests as tested (AST analysis)
	testedFuncs := make(map[string]bool)
	for _, tests := range fileTests {
		for _, test := range tests {
			for _, calledFunc := range test.CalledFuncs {
				testedFuncs[calledFunc] = true
			}
		}
	}

	var functionsWithoutTests []FuncInfo
	for _, funcs := range fileFunctions {
		for _, f := range funcs {
			key := f.Name
			if f.Receiver != "" {
				key = f.Receiver + "_" + f.Name
			}
			if !testedFuncs[key] && !testedFuncs[f.Name] {
				functionsWithoutTests = append(functionsWithoutTests, f)
			}
		}
	}

	// Sort by file then line
	sort.Slice(functionsWithoutTests, func(i, j int) bool {
		if functionsWithoutTests[i].File != functionsWithoutTests[j].File {
			return functionsWithoutTests[i].File < functionsWithoutTests[j].File
		}
		return functionsWithoutTests[i].Line < functionsWithoutTests[j].Line
	})

	// Analyze: find misplaced tests
	// A test is misplaced if it calls functions from a different source file
	var misplacedTests []MisplacedTest
	for testFile, tests := range fileTests {
		for _, test := range tests {
			if len(test.CalledFuncs) == 0 {
				continue
			}

			// Find which source file contains the called functions
			// Track which source files are called from this test
			sourceFileCounts := make(map[string]int)
			for _, calledFunc := range test.CalledFuncs {
				for sourceFile, funcs := range fileFunctions {
					for _, f := range funcs {
						funcKey := f.Name
						if f.Receiver != "" {
							funcKey = f.Receiver + "_" + f.Name
						}
						if funcKey == calledFunc || f.Name == calledFunc {
							sourceFileCounts[sourceFile]++
							break
						}
					}
				}
			}

			// Find the most called source file (primary source)
			var primarySource string
			maxCalls := 0
			for src, count := range sourceFileCounts {
				if count > maxCalls {
					maxCalls = count
					primarySource = src
				}
			}

			if primarySource != "" {
				// Expected test file is source_test.go
				expectedTestFile := strings.TrimSuffix(primarySource, ".go") + "_test.go"
				if testFile != expectedTestFile {
					// Check if the test file is in the same directory
					testDir := filepath.Dir(testFile)
					expectedDir := filepath.Dir(expectedTestFile)
					if testDir == expectedDir {
						misplacedTests = append(misplacedTests, MisplacedTest{
							Test:         test,
							ExpectedFile: expectedTestFile,
							ActualFile:   testFile,
						})
					}
				}
			}
		}
	}

	// Sort misplaced tests
	sort.Slice(misplacedTests, func(i, j int) bool {
		if misplacedTests[i].ActualFile != misplacedTests[j].ActualFile {
			return misplacedTests[i].ActualFile < misplacedTests[j].ActualFile
		}
		return misplacedTests[i].Test.Line < misplacedTests[j].Test.Line
	})

	return &AnalysisResult{
		FunctionsWithoutTests: functionsWithoutTests,
		MisplacedTests:        misplacedTests,
	}, nil
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
