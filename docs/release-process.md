# Release Process

Status: implemented and used. 0.1.0 was released with this pipeline on 2026-07-25
Last revised: 2026-07-25

How a version gets from a commit on the main branch to a binary someone downloads.

## Versioning

Semantic versioning, `MAJOR.MINOR.PATCH`, with the public surface defined precisely so that
"breaking" is not a judgement call.

**Public surface. Changing any of these is a MAJOR change:**

- Command names and action names.
- Flag names and flag semantics.
- JSON output field names, types, and structure.
- Exit code meanings.
- Configuration key names and types.

**MINOR**: new commands, new flags whose defaults preserve existing behaviour, new JSON fields,
new configuration keys with defaults, new platform support, and moving the Go version floor.

**PATCH**: bug fixes, performance work, documentation, dependency updates that change nothing
observable.

Notably: **output wording is not public surface.** Table column widths, message phrasing, and help
text improve freely. Anyone parsing human-readable output has been told to use `--output json`
and gets no compatibility promise.

Pre-1.0 versions carry no compatibility promise at all, which is the whole reason 1.0 is not
declared until the surface has been used enough to be confident about it.

## Deprecation

Nothing in the public surface is removed without warning. The sequence:

1. **One minor release ahead of removal**, the old form keeps working and prints a deprecation
   warning to stderr naming its replacement. Exit code and behaviour are unchanged.
2. The changelog entry for that release states what is deprecated and what replaces it.
3. Removal happens in the next major release, with a migration note.

A deprecation warning on stderr rather than stdout, so it cannot break a pipeline that is
otherwise still working.

## Branches

- `main` is always releasable. CI green, tests passing on all three platforms.
- Feature branches are short-lived and merge into `main`.
- Release branches (`release/1.2`) exist only when a patch is needed for an older minor version
  after `main` has moved on.
- Tags are `v1.2.3`, on the commit that is released.

## Preparing a release

Ordered, because several of these steps depend on the previous one.

**1. Confirm main is clean.** CI green on Windows, Linux, and macOS. No open regressions labelled
for this release.

**2. Run the full suite locally**, including integration and end-to-end tests, on at least one
platform. CI covers the rest, but the release manager should have seen it pass on their own
machine.

**3. Run the benchmarks** and compare against the committed baselines. A regression beyond the
threshold in `performance.md` blocks the release or gets an explicit written acceptance in the
changelog.

**4. Review the dependency audit.** Vulnerability scan clean, licenses still permissive, no
dependency added without its written justification in the pull request history.

**5. Update `CHANGELOG.md`.** Written by a person, in terms of what changed for a user. A
generated commit list is not a changelog: nobody reads "bump internal walker buffer" and learns
anything.

**6. Verify the documentation.** Every new command documented in `commands.md` and
`cli-reference.md`. Every changed behaviour reflected. Examples tested.

**7. Tag.** An annotated tag, `v1.2.3`, on the release commit.

**8. CI builds and publishes.** Pushing an annotated tag triggers
`.github/workflows/release.yml`, which verifies, builds, publishes, and then downloads what it
published and runs it.

Before tagging, the pipeline can be run end to end without publishing anything:

```
make release-check      # the configuration is valid
make release-snapshot   # every archive, package, and manifest, built locally
```

## Build

Release binaries are built by CI from the tagged commit. Never from a maintainer's machine:
a local build carries whatever that machine happened to have installed, and reproducibility is
the entire point.

The pipeline is [GoReleaser](https://goreleaser.com), configured in `.goreleaser.yaml`. It is a
build tool fetched on demand, pinned to a version, and it never enters the module graph: the
allow list in CI still covers every dependency DevNest actually links. What it replaced was
roughly four hundred lines of shell for archives, `.deb` and `.rpm` construction, checksums, and
manifest generation, none of which would have been better for being ours.

**Targets:**

| Platform | Architectures |
|---|---|
| Windows | amd64, arm64 |
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |

**Build settings:**

- `CGO_ENABLED=0` for static binaries with no runtime dependency. This is what makes "download and
  run" true on a locked-down corporate machine.
- `-trimpath`, so build paths from the CI runner do not end up in the binary.
- `-ldflags "-s -w"` to strip debug information and the symbol table, which is a substantial part
  of staying under the 25 MB size target.
- Version, commit hash, and build date injected at link time into `internal/version`.
- Reproducible: the same tag built twice produces identical bytes.

**Verification.** The workflow has three jobs, in order:

1. **verify** runs the unit and end-to-end suites on Windows, Linux, and macOS against the tagged
   commit. A tag can point at a commit CI never saw, and finding that out after the archives are
   published is finding out too late.
2. **release** builds every target and publishes.
3. **smoke** downloads what was published on each of the three platforms, verifies the checksum
   the way `installation.md` tells a user to, extracts the archive, runs `devnest version`, checks
   that the version reported matches the tag, and runs the end-to-end suite against that binary.

The third job runs after publishing because the artifacts have to exist to be downloaded. A
failure there means deleting the release, which costs nothing before anybody has installed it.

## Publishing

**Release artifacts:**

- One archive per platform and architecture: `.zip` for Windows, `.tar.gz` for Linux and macOS.
- `checksums.txt` with SHA-256 for every artifact.
- Release notes generated from the changelog section for this version.

**Package channels:**

- `.deb` and `.rpm` for amd64 and arm64, built and published with the release.
- The winget manifests and the Homebrew cask are generated and attached to the release, but not
  submitted anywhere yet. Both publishers are switched off in `.goreleaser.yaml` because neither
  target exists: turning them on is one field each, described below.
- `go install` works directly from the tag with no extra step.

**Turning on the Homebrew tap:** create `zeeqsleepy/homebrew-devnest`, add a token with write
access to it as the `HOMEBREW_TAP_TOKEN` secret, and set `skip_upload: false` under
`homebrew_casks`. The tap is self-hosted rather than in Homebrew core for the same reason the
Linux distribution repositories are out: both require an OSI-approved licence, and the Commons
Clause means DevNest's is not one.

**Turning on winget:** fork `microsoft/winget-pkgs`, add a token for it as `WINGET_TOKEN`, and set
`skip_upload: false` under `winget`. Each release then opens a pull request against the fork,
which is submitted upstream by hand.

Package channels lag the release by design. If something is wrong with a binary, it is easier to
fix before three package managers have distributed it.

## After publishing

- Verify the download and run path on each platform, from a clean machine or a clean container.
- Confirm checksums match.
- Confirm package channels resolve to the new version.
- Close the milestone.
- Open the next milestone.

## Patch releases

For a bug important enough not to wait: branch from the release tag, cherry-pick the fix, tag a
patch version, and let the release workflow run.

A patch release contains the fix and nothing else. Bundling "one more small thing" into a patch
release is how a patch release becomes the thing that needed patching.

## Hotfix for a security issue

The same as a patch release, with three differences:

1. Work happens in a private fork until the fix is ready, so the vulnerability is not disclosed by
   the commit history before users can update.
2. Coordinated disclosure: the advisory publishes with the release, not before.
3. All supported minor versions get the fix, not only the latest.

Supported versions and the private reporting channel are stated in `SECURITY.md`.

## Release checklist

```
[ ] CI green on Windows, Linux, macOS
[ ] Full test suite passing locally
[ ] Benchmarks within threshold
[ ] Dependency audit clean
[ ] CHANGELOG.md updated, written by hand
[ ] Documentation current, examples tested
[ ] Version tagged
[ ] CI build succeeded for all six targets
[ ] Binaries verified on each platform
[ ] Checksums published
[ ] Package channels updated
[ ] Milestone closed
```
