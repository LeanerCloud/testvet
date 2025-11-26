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

	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 79))

	// Summary
	fmt.Printf("Summary: %d functions without tests, %d misplaced tests\n",
		len(result.FunctionsWithoutTests), len(result.MisplacedTests))
}
