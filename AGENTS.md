# Agent Instructions

This file provides instructions for AI agents working on this Go SDK repository. The primary focus is the dependency update workflow.

## Project Overview

- **Language**: Go
- **Minimum Go Version**: 1.17 (see `go.mod`)
- **Module**: `github.com/ht-sdks/events-sdk-go`
- **Package Name**: `htevents`
- **CI**: GitHub Actions (`.github/workflows/ci.yml`), tested on Go 1.17, 1.19, and 1.22

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

If a dependency upgrade requires a newer minimum Go version, update the `go` directive in `go.mod`, update the CI matrix in `.github/workflows/ci.yml`, and update the supported versions listed in this `AGENTS.md` file accordingly.

---

## Troubleshooting Dependency Updates

### Go Version Compatibility

This module targets Go 1.17 as the minimum version (see `go.mod`). When upgrading dependencies:

- Verify the dependency still supports Go 1.17
- If it doesn't, either pin an older version or bump the minimum Go version in `go.mod` and update the CI matrix

### CI Failures After Dependency Updates

1. **Compilation errors**: A dependency may have dropped support for older Go versions. Check the dependency's `go.mod` for its minimum Go version.
2. **Test failures**: Review the changelog of updated packages for breaking changes or behavioral differences.
3. **Vet failures**: New versions of Go may introduce stricter vet checks. Run `go vet ./...` locally and fix any issues.

### Breaking API Changes in Dependencies

When upgrading major versions, APIs may change:

```bash
# Find all usages of a specific package
grep -r "packagename" --include="*.go"

# After updating, let the compiler tell you what broke
go build ./...
```

---

## Quick Reference

| Task | Command |
|------|---------|
| Download dependencies | `go get -v -t ./...` |
| Run linter/vet | `go vet ./...` |
| Run tests | `go test -race -count=1 ./...` |
| Build all packages | `go build ./...` |
| Run full CI locally | `make ci` |
| Check for updates | `go list -m -u all` |
| Update all deps | `go get -u ./... && go mod tidy` |
| Update specific dep | `go get github.com/pkg@version && go mod tidy` |
| Vulnerability scan | `govulncheck ./...` |
