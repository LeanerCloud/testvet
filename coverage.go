package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// analyzeCoverage runs go test with coverage and returns functions below the threshold
func analyzeCoverage(dir string, threshold float64, verbose bool) ([]LowCoverageFunc, error) {
	// Create temporary file for coverage profile
	tmpFile, err := os.CreateTemp("", "coverage-*.out")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Run go test with coverage
	if verbose {
		fmt.Fprintf(os.Stderr, "Running: go test -coverprofile=%s ./...\n", tmpPath)
	}

	cmd := exec.Command("go", "test", "-coverprofile="+tmpPath, "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if tests failed vs other errors
		if _, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go test failed: %s", stderr.String())
		}
		return nil, fmt.Errorf("failed to run go test: %w", err)
	}

	// Run go tool cover to get function coverage
	if verbose {
		fmt.Fprintf(os.Stderr, "Running: go tool cover -func=%s\n", tmpPath)
	}

	cmd = exec.Command("go", "tool", "cover", "-func="+tmpPath)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run go tool cover: %w\n%s", err, stderr.String())
	}

	// Parse output and filter by threshold
	return parseCoverageOutput(stdout.String(), dir, threshold)
}

// parseCoverageOutput parses go tool cover -func output
// Format: file:line:	funcName		percentage%
func parseCoverageOutput(output, baseDir string, threshold float64) ([]LowCoverageFunc, error) {
	var result []LowCoverageFunc

	// Regex to match coverage lines
	// Example: github.com/user/pkg/file.go:20:	funcName		85.7%
	re := regexp.MustCompile(`^(.+):(\d+):\s+(\S+)\s+(\d+\.?\d*)%$`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		// Skip total line
		if strings.HasPrefix(line, "total:") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		filePath := matches[1]
		lineNum, _ := strconv.Atoi(matches[2])
		funcName := matches[3]
		coverage, _ := strconv.ParseFloat(matches[4], 64)

		// Skip if above threshold
		if coverage >= threshold {
			continue
		}

		// Skip main and init functions (typically not unit tested)
		if funcName == "main" || funcName == "init" {
			continue
		}

		// Convert absolute path to relative
		relPath := filePath
		if abs, err := filepath.Abs(baseDir); err == nil {
			if rel, err := filepath.Rel(abs, filePath); err == nil && !strings.HasPrefix(rel, "..") {
				relPath = rel
			}
		}

		// Try to extract just the file path from module path
		// e.g., github.com/user/pkg/file.go -> file.go (if in same dir)
		parts := strings.Split(filePath, "/")
		if len(parts) > 0 {
			fileName := parts[len(parts)-1]
			// Check if file exists in the directory
			if _, err := os.Stat(filepath.Join(baseDir, fileName)); err == nil {
				relPath = fileName
			}
		}

		result = append(result, LowCoverageFunc{
			File:      relPath,
			Line:      lineNum,
			Name:      funcName,
			Coverage:  coverage,
			Threshold: threshold,
		})
	}

	// Sort by file, then line
	sort.Slice(result, func(i, j int) bool {
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		return result[i].Line < result[j].Line
	})

	return result, nil
}
