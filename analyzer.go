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

func analyzeProject(dir string, excludePrivate, verbose bool, coverageMap map[string]float64) (*AnalysisResult, error) {
	parsed, err := parseProjectFiles(dir, excludePrivate, verbose)
	if err != nil {
		return nil, err
	}

	testedFuncs := buildTestedFuncsMap(parsed.fileTests)
	functionsWithoutTests := findFunctionsWithoutTests(parsed.fileFunctions, testedFuncs, coverageMap)
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
// If coverageMap is provided, functions with >=50% coverage are considered adequately tested
func findFunctionsWithoutTests(fileFunctions map[string][]FuncInfo, testedFuncs map[string]bool, coverageMap map[string]float64) []FuncInfo {
	const coverageThreshold = 50.0 // Functions with >= 50% coverage are considered tested

	var result []FuncInfo
	for _, funcs := range fileFunctions {
		for _, f := range funcs {
			if !isFunctionTested(f, testedFuncs) {
				// If we have coverage data, skip functions with >= 50% coverage
				if coverageMap != nil {
					if cov, exists := coverageMap[f.Name]; exists && cov >= coverageThreshold {
						continue // Function has adequate coverage, skip it
					}
				}
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
	// Direct match by function name
	if testedFuncs[f.Name] {
		return true
	}

	// Match by receiver type + function name (e.g., autoScalingGroup_loadConfig)
	if f.Receiver != "" {
		key := f.Receiver + "_" + f.Name
		if testedFuncs[key] {
			return true
		}
	}

	// Check if any called function ends with _FunctionName
	// This handles cases where the variable name differs from the type name
	// e.g., asg.loadConfig() is extracted as "asg_loadConfig" but the receiver type is "autoScalingGroup"
	suffix := "_" + f.Name
	for calledFunc := range testedFuncs {
		if strings.HasSuffix(calledFunc, suffix) {
			return true
		}
	}

	return false
}

// findMisplacedTests finds tests that are in the wrong file
func findMisplacedTests(fileTests map[string][]TestInfo, fileFunctions map[string][]FuncInfo) []MisplacedTest {
	var result []MisplacedTest

	// Build a map of functions that are properly tested (have tests in the correct file)
	properlyTestedFuncs := buildProperlyTestedFuncsMap(fileTests, fileFunctions)

	for testFile, tests := range fileTests {
		for _, test := range tests {
			if misplaced := checkTestPlacement(test, testFile, fileFunctions, properlyTestedFuncs); misplaced != nil {
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

// buildProperlyTestedFuncsMap returns a set of function names that have tests in the correct file
func buildProperlyTestedFuncsMap(fileTests map[string][]TestInfo, fileFunctions map[string][]FuncInfo) map[string]bool {
	properlyTested := make(map[string]bool)

	for sourceFile, funcs := range fileFunctions {
		expectedTestFile := strings.TrimSuffix(sourceFile, ".go") + "_test.go"
		tests, hasTests := fileTests[expectedTestFile]
		if !hasTests {
			continue
		}

		// Collect all functions called from tests in the expected file
		calledInExpectedFile := make(map[string]bool)
		for _, test := range tests {
			for _, called := range test.CalledFuncs {
				calledInExpectedFile[called] = true
				// Also add the base function name for method calls like "asg_loadConfig" -> "loadConfig"
				if idx := strings.LastIndex(called, "_"); idx > 0 {
					calledInExpectedFile[called[idx+1:]] = true
				}
			}
		}

		// Mark functions from this source file as properly tested if called
		for _, f := range funcs {
			funcKey := f.Name
			if f.Receiver != "" {
				funcKey = f.Receiver + "_" + f.Name
			}
			if calledInExpectedFile[f.Name] || calledInExpectedFile[funcKey] {
				properlyTested[f.Name] = true
				properlyTested[funcKey] = true
			}
		}
	}

	return properlyTested
}

// checkTestPlacement checks if a test is in the correct file
func checkTestPlacement(test TestInfo, testFile string, fileFunctions map[string][]FuncInfo, properlyTestedFuncs map[string]bool) *MisplacedTest {
	if len(test.CalledFuncs) == 0 {
		return nil
	}

	// First, try to find the function under test by naming convention
	// TestFoo -> Foo, TestFoo_SubTest -> Foo, Test_Foo -> Foo
	primarySource := findSourceByTestName(test.Name, test.CalledFuncs, fileFunctions)

	// Fall back to counting unique called functions per file
	// Exclude functions that are already tested in their proper files
	if primarySource == "" {
		primarySource = findPrimarySourceFile(test.CalledFuncs, fileFunctions, properlyTestedFuncs)
	}

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

// findSourceByTestName tries to find the function under test by extracting
// the function name from the test name (e.g., TestFoo -> Foo)
func findSourceByTestName(testName string, calledFuncs []string, fileFunctions map[string][]FuncInfo) string {
	candidates := extractFunctionNamesFromTest(testName)
	if len(candidates) == 0 {
		return ""
	}

	// Extract potential receiver type from test name (e.g., Test_autoScalingGroup_method -> autoScalingGroup)
	receiverType := extractReceiverTypeFromTest(testName)

	// Try each candidate function name
	for _, funcName := range candidates {
		if sourceFile := tryMatchFunctionName(funcName, receiverType, calledFuncs, fileFunctions); sourceFile != "" {
			return sourceFile
		}
	}

	return ""
}

// extractReceiverTypeFromTest extracts the receiver type from test names like:
// Test_autoScalingGroup_method -> autoScalingGroup
// TestAutoScalingGroup_Method -> AutoScalingGroup
func extractReceiverTypeFromTest(testName string) string {
	if !strings.HasPrefix(testName, "Test") {
		return ""
	}
	name := testName[4:]
	if strings.HasPrefix(name, "_") {
		name = name[1:]
	}
	if name == "" {
		return ""
	}

	// Split by underscore - first part might be receiver type
	parts := strings.Split(name, "_")
	if len(parts) >= 2 {
		// Return the first part as potential receiver type
		return parts[0]
	}
	return ""
}

// tryMatchFunctionName tries to match a function name against called functions and source files
// receiverType is an optional hint from the test name (e.g., "autoScalingGroup" from Test_autoScalingGroup_method)
func tryMatchFunctionName(funcName, receiverType string, calledFuncs []string, fileFunctions map[string][]FuncInfo) string {
	// Check if this function was actually called in the test
	matchedName := ""
	funcNameLower := strings.ToLower(funcName)

	for _, called := range calledFuncs {
		calledLower := strings.ToLower(called)

		// Exact match (case-insensitive)
		if calledLower == funcNameLower {
			matchedName = called
			break
		}

		// Match "Type_FuncName" patterns (case-insensitive)
		if strings.HasSuffix(calledLower, "_"+funcNameLower) {
			matchedName = called
			break
		}

		// Prefix match: TestLoadDefaultConf -> loadDefaultConfig
		// funcNameLower "loaddefaultconf" is prefix of "loaddefaultconfig"
		if strings.HasPrefix(calledLower, funcNameLower) {
			matchedName = called
			break
		}

		// Handle Type_Method pattern: extract method part and check prefix
		if idx := strings.LastIndex(calledLower, "_"); idx > 0 {
			methodPart := calledLower[idx+1:]
			if strings.HasPrefix(methodPart, funcNameLower) {
				matchedName = called
				break
			}
		}
	}

	if matchedName == "" {
		return ""
	}

	// Find which source file contains this function
	// If receiverType is provided, prefer functions with matching receiver
	type match struct {
		file     string
		receiver string
	}
	var matches []match

	for sourceFile, funcs := range fileFunctions {
		for _, f := range funcs {
			if strings.EqualFold(f.Name, matchedName) || f.Name == matchedName {
				matches = append(matches, match{file: sourceFile, receiver: f.Receiver})
			}
			// Also match if the matched name includes receiver (e.g., "asg_loadDefaultConfig")
			if strings.HasSuffix(strings.ToLower(matchedName), "_"+strings.ToLower(f.Name)) {
				matches = append(matches, match{file: sourceFile, receiver: f.Receiver})
			}
		}
	}

	if len(matches) == 0 {
		return ""
	}

	// If we have a receiver type hint from the test name, prefer matching receiver
	if receiverType != "" {
		for _, m := range matches {
			if strings.EqualFold(m.receiver, receiverType) {
				return m.file
			}
		}
	}

	// Return the first match if no receiver preference or no receiver match
	return matches[0].file
}

// extractFunctionNameFromTest extracts candidate function names from a test name
// Returns multiple candidates to try, in order of preference:
// TestFoo -> [Foo]
// TestFoo_Bar -> [Foo, Bar] (Bar might be the method if Foo is a type)
// Test_Foo_Bar -> [Foo, Bar]
// TestAutoScalingGroup_needReplaceOnDemandInstances -> [AutoScalingGroup, needReplaceOnDemandInstances]
func extractFunctionNamesFromTest(testName string) []string {
	// Remove "Test" prefix
	if !strings.HasPrefix(testName, "Test") {
		return nil
	}
	name := testName[4:]

	// Handle Test_Foo pattern
	if strings.HasPrefix(name, "_") {
		name = name[1:]
	}

	if name == "" {
		return nil
	}

	// Split by underscore to get all parts
	parts := strings.Split(name, "_")
	if len(parts) == 1 {
		return []string{parts[0]}
	}

	// Return both the first part (might be type name) and subsequent parts (might be method names)
	// For TestAutoScalingGroup_needReplaceOnDemandInstances:
	// parts = [AutoScalingGroup, needReplaceOnDemandInstances]
	// We want to try needReplaceOnDemandInstances first (more specific), then AutoScalingGroup
	var candidates []string
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			candidates = append(candidates, parts[i])
		}
	}
	return candidates
}

// extractFunctionNameFromTest extracts the function name from a test name (legacy, returns first candidate)
func extractFunctionNameFromTest(testName string) string {
	candidates := extractFunctionNamesFromTest(testName)
	if len(candidates) == 0 {
		return ""
	}
	// Return the last candidate (first part after Test) for backward compatibility
	return candidates[len(candidates)-1]
}

// findPrimarySourceFile finds the source file with the most called functions
// It excludes functions that are already properly tested in their expected file
func findPrimarySourceFile(calledFuncs []string, fileFunctions map[string][]FuncInfo, properlyTestedFuncs map[string]bool) string {
	sourceFileCounts := make(map[string]int)

	for _, calledFunc := range calledFuncs {
		// Skip functions that are already tested in their proper file
		if properlyTestedFuncs[calledFunc] {
			continue
		}
		// Also check the base name for method calls like "asg_foo" -> "foo"
		if idx := strings.LastIndex(calledFunc, "_"); idx > 0 {
			baseName := calledFunc[idx+1:]
			if properlyTestedFuncs[baseName] {
				continue
			}
		}

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
