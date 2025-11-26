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

	flag.StringVar(&dir, "dir", ".", "Directory to analyze")
	flag.BoolVar(&excludePrivate, "exclude-private", false, "Exclude private (unexported) functions from analysis")
	flag.BoolVar(&verbose, "verbose", false, "Show verbose output")
	flag.Parse()

	// Convert to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving directory: %v\n", err)
		os.Exit(1)
	}

	result, err := analyzeProject(absDir, excludePrivate, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing project: %v\n", err)
		os.Exit(1)
	}

	printResults(result, absDir)
}
