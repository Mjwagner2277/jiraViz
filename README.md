# JiraViz

JiraViz renders an SVG program portfolio report from a Jira CSV export.

## Local Build

```bash
go build -o jiraviz .
./jiraviz -version
```

## Render a Report

```bash
go run . -input mockups/house-renovation-jira.csv -list-projects
go run . -input mockups/house-renovation-jira.csv -project REN -out mockups/generated-house-renovation.svg
```

## Security Scanning and Release Artifacts

The GitHub Actions release workflow builds the CLI and uses Trivy to scan both the repository and the compiled binary. It uploads SARIF to GitHub code scanning, uploads scan outputs as workflow artifacts, and attaches the same files to GitHub Releases when a release is published.

Artifacts are written under `dist/`:

```text
dist/jiraviz
dist/SHA256SUMS
dist/security/trivy-repository.txt
dist/security/trivy-repository.json
dist/security/trivy-repository.sarif
dist/security/jiraviz-repository.cdx.json
dist/security/trivy-cli-binary.txt
dist/security/trivy-cli-binary.json
dist/security/scan-manifest.txt
```

The workflow fails on `CRITICAL,HIGH` Trivy findings after writing the security artifacts.
