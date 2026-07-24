# Release Process

Status: draft; no release has been made yet
Last revised: 2026-07-23

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

**8. CI builds and publishes.** Tag push triggers the release workflow.

## Build

Release binaries are built by CI from the tagged commit. Never from a maintainer's machine:
a local build carries whatever that machine happened to have installed, and reproducibility is
the entire point.

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

**Verification, before anything is published:**

- Each binary runs `--version` on its target platform and reports the expected version.
- The end-to-end suite runs against the built binary, not against `go run`.
- Binary size is checked against the target.

## Publishing

**Release artifacts:**

- One archive per platform and architecture: `.zip` for Windows, `.tar.gz` for Linux and macOS.
- `checksums.txt` with SHA-256 for every artifact.
- Release notes generated from the changelog section for this version.

**Package channels**, updated after the release artifacts are published and verified:

- winget manifest for Windows.
- Homebrew formula for macOS and Linux.
- `.deb` and `.rpm` packages.
- `go install` works directly from the tag with no extra step.

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
