<!-- Parent: ../../AGENTS.md -->

# .github/workflows/

## Purpose

CI pipeline configuration for databricks-opencode.

## Files

| File | Purpose |
|------|---------|
| `ci.yml` | GitHub Actions workflow: runs on pull requests to master; tests, vet, build |

## Workflow: ci.yml

**Trigger**: Pull requests to `master` branch

**Steps**:
1. Checkout code
2. Setup Go from go.mod version (1.22)
3. Run tests: `go test ./... -v`
4. Run linter: `go vet ./...`
5. Build binary: `go build -o databricks-opencode .`

All steps must pass for PR approval.
