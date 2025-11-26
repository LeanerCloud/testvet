# testvet

A Go static analysis tool that identifies missing test coverage and misplaced test functions in your Go projects.

## Features

- **Missing Test Detection**: Finds functions and methods not called from any test function
- **AST-Based Call Analysis**: Analyzes actual function calls in tests, not naming conventions
- **Misplaced Test Detection**: Identifies tests that primarily call functions from a different source file
- **Method Support**: Handles methods with receivers, including generics
- **Flexible Filtering**: Option to exclude private (unexported) functions from analysis
- **Clean Output**: Organized results grouped by file with line numbers

## Installation

```bash
go install github.com/LeanerCloud/testvet@latest
```

Or build from source:

```bash
git clone https://github.com/LeanerCloud/testvet.git
cd testvet
go build -o testvet .
```

## Usage

```bash
# Analyze current directory
testvet

# Analyze a specific directory
testvet -dir /path/to/your/project

# Exclude private (unexported) functions
testvet -dir . -exclude-private

# Show verbose output (parse warnings)
testvet -dir . -verbose
```

## Example Output

```
================================================================================
GO TEST COVERAGE ANALYSIS
================================================================================
Project: /path/to/your/project

--------------------------------------------------------------------------------
FUNCTIONS WITHOUT TEST COVERAGE (3)
--------------------------------------------------------------------------------

handlers/user.go:
  Line 25: CreateUser
  Line 48: (UserService).ValidateEmail

utils/helpers.go:
  Line 12: parseConfig

--------------------------------------------------------------------------------
MISPLACED TESTS (1)
--------------------------------------------------------------------------------

TestCreateUser (line 15):
  Current file:  handlers/api_test.go
  Expected file: handlers/user_test.go

================================================================================
Summary: 3 functions without tests, 1 misplaced tests
```

## How It Works

1. **Parsing**: Uses Go's `go/ast` package to parse all `.go` files in the target directory
2. **Function Extraction**: Extracts all function and method declarations from source files
3. **Test Extraction**: Identifies test functions (`Test*`, `Benchmark*`, `Example*`, `Fuzz*`) from `_test.go` files
4. **Call Analysis**: Walks the AST of each test function to find all function calls within it
5. **Matching**: A function is considered tested if it's called from any test function
6. **Misplacement Detection**: A test is misplaced if it primarily calls functions from a different source file

### Excluded from Analysis

- `main()` and `init()` functions
- `vendor/` directory
- `testdata/` directory
- Hidden directories (starting with `.`)

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | `.` | Directory to analyze |
| `-exclude-private` | `false` | Exclude unexported functions from analysis |
| `-verbose` | `false` | Show verbose output including parse warnings |
| `-threshold` | `0` | Show functions with statement coverage below this percentage (0 to disable, excludes main/init) |

## testvet vs go test -cover

These tools measure different aspects of test coverage:

| Aspect | testvet | go test -cover |
|--------|---------|----------------|
| **Question answered** | Is this function called from any test? | What percentage of statements are executed? |
| **Granularity** | Per function (binary: yes/no) | Per statement (percentage) |
| **Use case** | Find completely untested functions | Measure thoroughness of existing tests |

**Example**: A function with complex error handling might show:
- testvet: ✓ covered (called from a test)
- go test -cover: 60% (only the happy path is tested)

Both metrics are valuable:
- Use **testvet** to find functions with zero test coverage
- Use **go test -cover** to measure how thoroughly each function is tested

```bash
# Find untested functions
testvet .

# Measure statement coverage
go test -cover ./...

# View detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Limitations

- AST analysis only detects direct function calls within test functions (not calls from helper functions)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.
