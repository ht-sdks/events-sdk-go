# Agent Instructions

This file provides instructions for AI agents working on this Go SDK repository.

## Project Overview

- **Language**: Go
- **Minimum Go Version**: 1.17 (see `go.mod`)
- **Module**: `github.com/ht-sdks/events-sdk-go`
- **Package Name**: `htevents`
- **Testing**: Go standard `testing` package with `go test -race`
- **Linting**: `go vet`
- **Build**: `go build`
- **CI**: GitHub Actions (`.github/workflows/ci.yml`)

### Project Structure

```
.
├── client.go              # Core client (version constant, HTTP transport, batch upload)
├── config.go              # Client configuration (endpoints, intervals, batch sizes)
├── message.go             # Message types (base message interface)
├── track.go               # Track event type
├── identify.go            # Identify event type
├── group.go               # Group event type
├── page.go                # Page event type
├── screen.go              # Screen event type
├── alias.go               # Alias event type
├── context.go             # Event context (device, OS, library info)
├── properties.go          # Event properties helper
├── traits.go              # User traits helper
├── integrations.go        # Integrations configuration
├── validate.go            # Message validation logic
├── json.go                # Custom JSON marshaling
├── logger.go              # Logging interface
├── executor.go            # Async executor
├── error.go               # Error types
├── timeout_15.go          # HTTP timeout for Go <1.6
├── timeout_16.go          # HTTP timeout for Go ≥1.6
├── cmd/cli/main.go        # CLI tool for sending events
├── examples/track.go      # Usage example
├── fixtures/              # JSON test fixtures for message serialization
├── .buildscript/          # Legacy bootstrap script
├── Makefile               # Build/test/CI targets
└── .github/workflows/     # GitHub Actions CI configuration
```

### Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/google/uuid` | Generating unique message IDs |
| `github.com/segmentio/backo-go` | Exponential backoff for retries |
| `github.com/segmentio/conf` | CLI configuration (used by `cmd/cli`) |

---

## Updating Dependencies

### 1. Pre-flight Checks

```bash
# Check Go version
go version

# Ensure you're at the repository root
pwd  # Should be: /path/to/events-sdk-go

# Verify go.mod exists and is correct
cat go.mod
```

### 2. Establish Test Baseline

```bash
# Download all dependencies
go get -v -t ./...

# Run vet (static analysis / linting)
go vet ./...

# Run all tests with race detection
go test -race -count=1 ./...
```

Record the number of passing tests before making any changes. This ensures you can verify nothing broke after upgrading.

### 3. Check for Vulnerabilities

```bash
# Install govulncheck if not present
go install golang.org/x/vuln/cmd/govulncheck@latest

# Scan for known vulnerabilities
govulncheck ./...
```

Review any reported vulnerabilities. Prioritize upgrading affected packages.

### 4. Check Outdated Packages

```bash
# List all dependencies and their versions
go list -m all

# Check for available updates (direct dependencies only)
go list -m -u all
```

This shows each module, the currently required version, and (if available) the latest version in brackets.

### 5. Upgrade Dependencies

#### Option A: Safe Updates (patch/minor within compatibility)

```bash
# Update all dependencies to latest minor/patch versions
go get -u ./...

# Tidy up go.mod and go.sum (remove unused, add missing)
go mod tidy
```

#### Option B: Update a Specific Package

```bash
# Update a specific direct dependency
go get github.com/google/uuid@latest

# Or pin to a specific version
go get github.com/google/uuid@v1.7.0

# Always tidy afterwards
go mod tidy
```

#### Option C: Major Version Updates

For major version bumps in Go modules, the import path changes (e.g., `v1` to `v2`). You must:

1. Update the import path in all `.go` files that reference the package
2. Update `go.mod` to require the new major version
3. Run `go mod tidy`

```bash
# Example: find all files importing the old version
grep -r "github.com/example/pkg" --include="*.go"

# Update imports in each file, then:
go mod tidy
```

### 6. Rebuild and Test

```bash
# Run vet (static analysis)
go vet ./...

# Run all tests with race detection
go test -race -count=1 ./...

# Build all packages (including cmd/cli)
go build ./...
```

Compare test results to the baseline from step 2. Fix any failures before proceeding.

### 7. Verify CI Would Pass

The CI matrix tests on Go 1.17, 1.19, and 1.22. If you have multiple Go versions available, test across them:

```bash
# The CI effectively runs:
make ci
# Which executes: go get -v -t ./... && go vet ./... && go test -race ./...
```

If a dependency upgrade requires a newer minimum Go version, update the `go` directive in `go.mod` and update the CI matrix in `.github/workflows/ci.yml` accordingly.

---

## Version Bumping

### Semantic Versioning

- **PATCH** (0.0.2 → 0.0.3): Bug fixes, dependency updates, no new features
- **MINOR** (0.0.2 → 0.1.0): New backwards-compatible features
- **MAJOR** (0.0.2 → 1.0.0): Breaking API changes

Dependency updates are typically **PATCH** bumps.

### Files to Update

1. `client.go` — Update the `Version` constant:
   ```go
   const Version = "X.Y.Z"
   ```
2. `fixtures/*.json` — Update version strings in test fixture files to match the new version (find and replace the old version string)
3. Create a new [GitHub Release](https://github.com/ht-sdks/events-sdk-go/releases) with a matching tag

See `RELEASE.md` for the full release process.

---

## CI/CD

- **CI config**: `.github/workflows/ci.yml`
- **Trigger**: Every push to any branch
- **Go versions tested**: 1.17, 1.19, 1.22
- **Steps**: `make ci` which runs `go get -v -t ./...` → `go vet ./...` → `go test -race`

### CI Failures After Dependency Updates

1. **Compilation errors**: A dependency may have dropped support for older Go versions. Check the dependency's `go.mod` for its minimum Go version.
2. **Test failures**: Review the changelog of updated packages for breaking changes or behavioral differences.
3. **Vet failures**: New versions of Go may introduce stricter vet checks. Run `go vet ./...` locally and fix any issues.

---

## Common Issues

### Go Version Compatibility

This module targets Go 1.17 as the minimum version (see `go.mod`). When upgrading dependencies:

- Verify the dependency still supports Go 1.17
- If it doesn't, either pin an older version or bump the minimum Go version in `go.mod` and update the CI matrix

### Breaking API Changes in Dependencies

When upgrading major versions, APIs may change:

```bash
# Find all usages of a specific package
grep -r "packagename" --include="*.go"

# After updating, let the compiler tell you what broke
go build ./...
```

### Test Fixtures

The `fixtures/` directory contains JSON files with expected serialized output, including the SDK version string. After bumping the version in `client.go`, update all fixture files:

```bash
# Find files containing the old version
grep -r "0.0.2" fixtures/
```

### Timeout Compatibility

The repo contains two timeout implementation files:

- `timeout_15.go` — For Go versions before 1.6 (uses build constraints)
- `timeout_16.go` — For Go 1.6+ (uses `http.Client.Timeout`)

These use build tags and should not need modification unless dropping support for very old Go versions.

---

## Quick Reference

| Task | Command |
|------|---------|
| Download dependencies | `go get -v -t ./...` |
| Run linter/vet | `go vet ./...` |
| Run tests | `go test -race -count=1 ./...` |
| Run tests with coverage | `go test -race -coverprofile=cover.out .` |
| View coverage report | `go tool cover -func cover.out` |
| Build all packages | `go build ./...` |
| Run full CI locally | `make ci` |
| Check for updates | `go list -m -u all` |
| Update all deps | `go get -u ./... && go mod tidy` |
| Update specific dep | `go get github.com/pkg@version && go mod tidy` |
| Vulnerability scan | `govulncheck ./...` |
| View module graph | `go mod graph` |

---

## Development Tips

### Running a Specific Test

```bash
# Run a single test function
go test -run TestClientEnqueueTrack -v .

# Run tests matching a pattern
go test -run TestClient -v .
```

### Using the CLI

```bash
# Build and run the CLI tool
go run ./cmd/cli -write-key YOUR_WRITE_KEY -type track -event "Test Event" -user-id "test-user"
```

### Race Detection

Always test with `-race` to catch data races:

```bash
go test -race ./...
```

### Viewing Documentation Locally

```bash
# Start a local godoc server
go install golang.org/x/tools/cmd/godoc@latest
godoc -http=:6060
# Then open http://localhost:6060/pkg/github.com/ht-sdks/events-sdk-go/
```
