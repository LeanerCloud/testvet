package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var dir string
	var excludePrivate bool
	var verbose bool
	var threshold float64
	var useCoverage bool

	flag.StringVar(&dir, "dir", ".", "Directory to analyze")
	flag.BoolVar(&excludePrivate, "exclude-private", false, "Exclude private (unexported) functions from analysis")
	flag.BoolVar(&verbose, "verbose", false, "Show verbose output")
	flag.Float64Var(&threshold, "threshold", 0, "Show functions with coverage below this percentage (0 to disable)")
	flag.BoolVar(&useCoverage, "use-coverage", true, "Use coverage data to filter out indirectly tested functions (runs go test)")
	flag.Parse()

	// Convert to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving directory: %v\n", err)
		os.Exit(1)
	}

	// Get coverage map if -use-coverage is set
	var coverageMap map[string]float64
	if useCoverage {
		coverageMap, err = getCoverageMap(absDir, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: coverage analysis failed: %v\n", err)
			// Continue without coverage filtering
		}
	}

	result, err := analyzeProject(absDir, excludePrivate, verbose, coverageMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing project: %v\n", err)
		os.Exit(1)
	}

	// Run coverage analysis if threshold is set
	if threshold > 0 {
		lowCoverage, err := analyzeCoverage(absDir, threshold, verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: coverage analysis failed: %v\n", err)
		} else {
			result.LowCoverageFuncs = lowCoverage
		}
	}

	printResults(result, absDir)
}
