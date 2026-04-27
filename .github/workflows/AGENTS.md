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

## Release Process (release-please)

Releases are automated via [release-please](https://github.com/googleapis/release-please). It watches commits merged to `master` and opens a release PR automatically — **but only when commits follow the Conventional Commits format**.

### Commit prefix rules

| Prefix | Version bump | When to use |
|--------|-------------|-------------|
| `feat:` | minor (0.X.0) | New user-facing feature or behaviour change |
| `fix:` | patch (0.0.X) | Bug fix |
| `feat!:` / `fix!:` / `BREAKING CHANGE:` | major (X.0.0) | Breaking API or behaviour change |
| `chore:`, `docs:`, `refactor:`, `test:` | none | Internal — **will NOT trigger a release PR** |

### Rules for AI agents

- **Every PR that changes user-facing behaviour must include at least one `feat:` or `fix:` commit.** Without it, release-please skips the run and no release PR is opened.
- Squash-merge is fine — the squash commit message is what release-please reads.
- `chore:` is safe for housekeeping (formatting, test updates, doc-only changes) but will not produce a release.
- If a release PR is unexpectedly missing after a merge, check the release workflow logs: the most common cause is a non-conventional commit message.
