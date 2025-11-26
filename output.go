package main

import (
	"fmt"
	"strings"
)

func printResults(result *AnalysisResult, baseDir string) {
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("GO TEST COVERAGE ANALYSIS")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Printf("Project: %s\n\n", baseDir)

	// Functions without tests
	fmt.Println("-" + strings.Repeat("-", 79))
	fmt.Printf("FUNCTIONS WITHOUT TEST COVERAGE (%d)\n", len(result.FunctionsWithoutTests))
	fmt.Println("-" + strings.Repeat("-", 79))

	if len(result.FunctionsWithoutTests) == 0 {
		fmt.Println("All functions have test coverage!")
	} else {
		currentFile := ""
		for _, f := range result.FunctionsWithoutTests {
			if f.File != currentFile {
				if currentFile != "" {
					fmt.Println()
				}
				currentFile = f.File
				fmt.Printf("\n%s:\n", f.File)
			}
			funcDesc := f.Name
			if f.Receiver != "" {
				funcDesc = fmt.Sprintf("(%s).%s", f.Receiver, f.Name)
			}
			fmt.Printf("  Line %d: %s\n", f.Line, funcDesc)
		}
	}

	fmt.Println()

	// Misplaced tests
	fmt.Println("-" + strings.Repeat("-", 79))
	fmt.Printf("MISPLACED TESTS (%d)\n", len(result.MisplacedTests))
	fmt.Println("-" + strings.Repeat("-", 79))

	if len(result.MisplacedTests) == 0 {
		fmt.Println("All tests are in the correct files!")
	} else {
		for _, mt := range result.MisplacedTests {
			fmt.Printf("\n%s (line %d):\n", mt.Test.Name, mt.Test.Line)
			fmt.Printf("  Current file:  %s\n", mt.ActualFile)
			fmt.Printf("  Expected file: %s\n", mt.ExpectedFile)
		}
	}

	// Low coverage functions (if threshold was set)
	if len(result.LowCoverageFuncs) > 0 {
		fmt.Println()
		fmt.Println("-" + strings.Repeat("-", 79))
		threshold := result.LowCoverageFuncs[0].Threshold
		fmt.Printf("LOW COVERAGE FUNCTIONS (below %.1f%%) (%d)\n", threshold, len(result.LowCoverageFuncs))
		fmt.Println("-" + strings.Repeat("-", 79))

		currentFile := ""
		for _, f := range result.LowCoverageFuncs {
			if f.File != currentFile {
				if currentFile != "" {
					fmt.Println()
				}
				currentFile = f.File
				fmt.Printf("\n%s:\n", f.File)
			}
			fmt.Printf("  Line %d: %s (%.1f%%)\n", f.Line, f.Name, f.Coverage)
		}
	}

	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 79))

	// Summary
	summary := fmt.Sprintf("Summary: %d functions without tests, %d misplaced tests",
		len(result.FunctionsWithoutTests), len(result.MisplacedTests))
	if len(result.LowCoverageFuncs) > 0 {
		summary += fmt.Sprintf(", %d low coverage functions", len(result.LowCoverageFuncs))
	}
	fmt.Println(summary)
}
