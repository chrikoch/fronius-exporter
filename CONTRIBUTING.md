# Contributing

## Developing

### Requirements

* Go
* Docker

### Building

Run `make help` to see available goals

Platform related builds are handled by `goreleaser`:

```
export GOOS=linux
export GOARCH=arm64
export IMAGE_REPOSITORY=chrikoch/fronius-exporter
goreleaser build --snapshot --clean --single-target
```

On the other hand you can also use the actual `make build` in combination with `docker buildx`

```
export GOOS=linux
export GOARCH=arm64
export IMAGE_REPOSITORY=chrikoch/fronius-exporter

make build
# you now have a binary with arm64 arch

docker buildx build --platform linux/arm64 -t $IMAGE_REPOSITORY --push .
```

### Test metrics

If you don't have a Symo device at hand, you can fake one:

```console
# In its own terminal
python3 -m http.server 8081

# In another terminal
go run . -v --symo.url http://localhost:8081/pkg/fronius/testdata/example_1.json

# Yet another terminal or browser
curl -L http://localhost:8080/metrics
```

## Release process

Release notes are generated from merged pull requests and their labels. The
categories and recognized labels are defined in
[.github/changelog-configuration.json](.github/changelog-configuration.json).
Use a matching label for each pull request that should appear in the notes.
Pull requests without a matching label and commits pushed directly to `master`
are not included in the generated notes.

When preparing a release:

1. Choose the version and identify the previous release tag and the commit to
   release. Ensure the release commit is on `master` and available on GitHub.
2. Run the relevant checks, including `make test`, and check the GitHub Actions
   results for the release commit.
3. Review the changes since the previous release, for example with
   `git log --oneline <previous-tag>..<release-commit>`. Identify direct commits,
   pull requests without a recognized label, and any user-facing changes that
   need an explanation in the release notes. Add appropriate PR labels before
   publishing where possible.
4. Create and push the version tag on the chosen commit. Pushing a tag starts
   the [release workflow](.github/workflows/release.yml), which generates notes
   from pull requests and publishes the release and artifacts.
5. Check that the workflow succeeded and the release and expected artifacts are
   available. Compare the published release notes with the changes reviewed in
   step 3. Edit the GitHub release notes to add missing relevant changes and
   explain compatibility implications. The generated count covers categorized
   pull requests, so adjust or remove it if the notes are supplemented.

The release workflow is managed by greposync; consult its configuration before
changing the workflow directly.
