# Contributing to JiraViz

## Commit Messages and Semantic Versioning

JiraViz uses Conventional Commit-style commit messages because semantic version tags are derived from commits on `main`.

The `Semantic Version Tag` workflow creates annotated `vMAJOR.MINOR.PATCH` tags from pushes to `main`.

Automatic bump rules:

- Major: commit body contains `BREAKING CHANGE:` or the subject uses a Conventional Commit bang, such as `feat!:` or `fix!:`
- Minor: commit subject starts with `feat:` or `feat(scope):`
- Patch: all other commits, including `fix:`, `docs:`, `chore:`, `ci:`, and `refactor:`

The workflow can also be run manually from GitHub Actions with a forced `patch`, `minor`, or `major` bump.

Examples:

- `fix: prevent epic percent labels from overlapping bars` creates a patch tag
- `feat: expand construction sample data` creates a minor tag
- `feat!: change CSV field mapping behavior` creates a major tag
- A commit body containing `BREAKING CHANGE:` creates a major tag

Choose the commit prefix intentionally so the next semantic tag is correct.
