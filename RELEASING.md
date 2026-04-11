# Release Process

This document describes the process for releasing a new version of OpenLoadBalancer.

## Versioning

OpenLoadBalancer follows [Semantic Versioning](https://semver.org/):

- **MAJOR**: Incompatible API changes
- **MINOR**: Backward-compatible functionality additions
- **PATCH**: Backward-compatible bug fixes

## Pre-Release Checklist

Before creating a release, ensure:

- [ ] All tests pass (`go test ./...`)
- [ ] Coverage is above 85%
- [ ] `gofmt -w .` and `go vet ./...` pass
- [ ] CHANGELOG.md is updated with release notes
- [ ] Documentation is updated (if needed)
- [ ] No open critical issues

## Release Steps

### 1. Prepare Release Branch

```bash
# Create release branch
git checkout -b release/v0.1.0

# Update version references (if any)
# Update CHANGELOG.md with release date
```

### 2. Update CHANGELOG.md

Move items from `[Unreleased]` to new version section:

```markdown
## [1.0.0] - 2025-04-04

### Added
- Feature description

### Fixed
- Bug fix description
```

### 3. Create Pull Request

```bash
git push origin release/v0.1.0
```

Create PR on GitHub with title: `Release v0.1.0`

### 4. Merge and Tag

After PR approval and merge:

```bash
git checkout main
git pull origin main

# Create signed tag
git tag -s v0.1.0 -m "Release v0.1.0"

# Push tag
git push origin v0.1.0
```

### 5. CI/CD Build

GitHub Actions will automatically:
- Build binaries for all platforms
- Create Docker images
- Create GitHub Release
- Attach release artifacts

### 6. Verify Release

- [ ] Binaries are available on GitHub Releases
- [ ] Docker images are published
- [ ] Documentation is updated
- [ ] Announcement is made (if applicable)

## Docker Images

Docker images are published to:
- `ghcr.io/openloadbalancer/olb:latest`
- `ghcr.io/openloadbalancer/olb:v0.1.0`

## Binary Artifacts

Binaries are built for:
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)
- FreeBSD (amd64)

## Post-Release

- Monitor for new issues
- Update website if needed
- Announce on social channels (if applicable)

## Emergency Releases

For critical security fixes:

1. Create hotfix branch from latest release tag
2. Apply fix with minimal changes
3. Fast-track review process
4. Release as PATCH version

## Rollback

If a release has critical issues:

1. Document issues in release notes
2. Recommend previous stable version
3. Create new PATCH release with fixes
