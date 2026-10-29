# Repository Guidelines

## Project structure

`main.go` and `metrics.go` implement the exporter. Configuration lives in
`cfg/`, and the Fronius API client in `pkg/fronius/`. Tests sit alongside the
code; API response fixtures are in `pkg/fronius/testdata/`. Examples, including
systemd configuration, are in `examples/`.

## Development and testing

- `make build` formats and vets the code, then builds the exporter binary.
- `make test` runs Go tests and writes the coverage profile to `cover.out`.
- `make run` starts the exporter locally; `make help` lists available targets.
- `make lint` runs formatting and vetting, then checks for a clean Git diff.

Use standard Go formatting (`go fmt ./...`) and name tests `*_test.go`. Tests
use Go's testing package and, where useful, `testify`. For testing metrics
without a device, follow the fixture-server instructions in
[CONTRIBUTING.md](CONTRIBUTING.md#test-metrics).

## Contributions and managed files

Use concise, descriptive commit subjects, as in the recent project history.
Follow the [pull request template](.github/PULL_REQUEST_TEMPLATE.md): summarize
the change, select a changelog label, update relevant documentation and tests,
and link related issues.

The Makefiles are managed by greposync; follow their headers instead of editing
them manually. Consult the greposync configuration before changing the release
workflow.

## Release instructions

Follow the [release process](CONTRIBUTING.md#release-process) when preparing or
publishing a release. In particular, compare the changes since the previous tag
with the generated release notes and ensure relevant direct commits and
uncategorized pull requests are documented.
