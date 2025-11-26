package main

// FuncInfo holds information about a function
type FuncInfo struct {
	Name     string
	File     string
	Line     int
	Receiver string // empty for regular functions, type name for methods
}

// TestInfo holds information about a test function
type TestInfo struct {
	Name        string
	File        string
	Line        int
	CalledFuncs []string // functions called within this test (from AST analysis)
}

// AnalysisResult holds the analysis results
type AnalysisResult struct {
	FunctionsWithoutTests []FuncInfo
	MisplacedTests        []MisplacedTest
	LowCoverageFuncs      []LowCoverageFunc
}

// LowCoverageFunc represents a function with coverage below the threshold
type LowCoverageFunc struct {
	File       string
	Line       int
	Name       string
	Coverage   float64
	Threshold  float64
}

// MisplacedTest represents a test in the wrong file
type MisplacedTest struct {
	Test         TestInfo
	ExpectedFile string
	ActualFile   string
}
