# Contributing Guide

Status: current as of Phase 10
Last revised: 2026-07-24

`development-guide.md` covers setting up and doing the work. This document covers getting a change
accepted.

`CONTRIBUTING.md` at the repository root is the short version for people arriving from a pull
request template. This is the full one.

## Before writing code

**For a bug fix**, open an issue if one does not exist, then go ahead. Small fixes do not need
discussion first.

**For a feature**, open an issue and get agreement before you start. This is not bureaucracy: a
feature can be rejected on scope grounds (see the non-goals in `prd.md`), and finding that out
after a week of work is a bad experience for everyone. A maintainer will say yes, no, or "yes but
differently", and all three save time.

**For a refactor**, open an issue explaining what is wrong with the current structure and what
would improve. Structural changes are the hardest to review and the easiest to get wrong, and a
large unsolicited refactor rarely lands.

**For documentation**, go ahead. Documentation fixes need no discussion.

## What gets accepted

Aligned with `prd.md` and `design.md`:

- Fixes for real defects, with a test that fails without the fix.
- Features within the stated scope, agreed in an issue first.
- New toolchain detections in `env`: a table entry each, and the most contribution-friendly area
  in the project.
- New patterns in `clean` and new rules in `secret`, with tests and a justification for why the
  pattern is safe or the rule does not produce false positives.
- Platform fixes, particularly Windows ones.
- Performance improvements with before-and-after benchmark numbers.
- Documentation improvements.

## What does not get accepted

Not because the work is bad, but because it is not what this project is:

- Anything in the non-goals list in `prd.md`: package management, daemons, TUIs, telemetry,
  plugin systems.
- New dependencies without the written justification required by `rules.md` R13.
- Large refactors that were not discussed first.
- Changes that break the public surface without a major version and a migration path.
- Features that only work on one platform, unless the feature is inherently platform-specific and
  degrades cleanly elsewhere.
- Performance changes with no measurement.
- Style changes that fight `gofmt` or the linter configuration.

## Pull requests

**One logical change per pull request.** A refactor and a feature in one branch cannot be reviewed
properly and cannot be reverted separately when one of them turns out to be wrong.

**CI must be green before review.** Reviewer time is for design and correctness, not for catching
lint errors. `make check` locally first.

**The description explains why.** What changed is visible in the diff. Why it changed, what
alternatives were considered, and what the reviewer should look at closely are not.

**Include:**

- The issue it closes.
- What changed and why.
- How it was tested, and on which platforms.
- Benchmark numbers, if it touches performance.
- The dependency justification, if it adds one.
- Screenshots or terminal output, if it changes what the user sees.

**Commits** use conventional prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `perf:`,
`build:`, `ci:`, `chore:`) with an imperative subject line under 72 characters. History is not
squashed on merge if the individual commits are meaningful; it is if they are "wip" and "fix
typo".

## Review

**What a reviewer checks, roughly in order:**

1. **Scope**: is this the change the issue agreed on?
2. **Layering**: does it respect the boundaries in `architecture.md`? Does a module import
   another module, print to stdout, or read configuration directly?
3. **Correctness**: including the error paths, which is where most defects hide.
4. **Safety**: for anything destructive, is every guard in `security.md` present and tested?
5. **Tests**: does a bug fix have a test that fails without it? Do tests assert on error codes
   rather than message text?
6. **Cross-platform**: path handling, separators, case sensitivity, long paths.
7. **Documentation**: updated in the same change, per `rules.md` R41.
8. **Style**: last, and least. `gofmt` and the linter have already handled most of it.

**Expectations of a reviewer:**

- Respond within a few days. A pull request sitting for weeks is a failure of the project, not of
  the contributor.
- Be specific. "This could be cleaner" is not actionable; "this would read better as a guard
  clause, see `coding-standard.md`" is.
- Distinguish blocking from non-blocking. Say which comments must be addressed and which are
  suggestions.
- Point at the document. If a rule is being violated, cite it, that way the rule gets learned
  rather than the individual comment.
- Approve when it is good enough. Perfect is not the bar; better than what is on `main` is.

**Expectations of a contributor:**

- Respond to comments, even if only to disagree. A comment left unanswered stalls the review.
- Disagreement is fine and often correct. Explain the reasoning; a maintainer decides if it stays
  unresolved.
- Push fixes as new commits during review rather than force-pushing, so the reviewer can see what
  changed. Squash at the end if the history is not meaningful.
- Ask if a comment is unclear.

## Rules that get enforced mechanically

These fail the build, so there is no point arguing them in review:

- `gofmt` formatting.
- `go vet` and the linter configuration in `tools/`.
- Import boundaries from `architecture.md`.
- Race detector on all tests.
- Coverage floor of 80% on `internal/core`.
- Tests passing on Windows, Linux, and macOS.
- License check on dependencies.
- Every command having a description and at least two examples.

## Reporting bugs

Include:

- The `devnest` version: `devnest version`.
- The operating system and version.
- The exact command that was run.
- What was expected and what happened.
- The output of `devnest doctor`.
- The output with `--verbose`, if it is relevant.

Redact anything sensitive before pasting. Paths often contain names.

## Reporting a security issue

Do not open a public issue. Use the private channel in `SECURITY.md`.

## Conduct

`CODE_OF_CONDUCT.md` applies to every project space. The short version: assume good faith, keep
criticism aimed at the code, and remember that the person on the other end is doing this
voluntarily.

## Licensing

Contributions are licensed under the project's licence: Apache License 2.0 with the Commons Clause,
which is Apache 2.0 in full minus the right to sell the software. By opening a pull request you
confirm you have the right to contribute the code and agree to that licence. There is no CLA.
