#!/usr/bin/env bash
# Extract one version's section from CHANGELOG.md, for use as release notes.
#
# The changelog is written by a person and is the description of a release that
# anybody would want to read. A generated commit list is not: it repeats what
# the commits already say, and GoReleaser's version of it prints every author's
# email address into a public page.
#
# Usage: scripts/release-notes.sh 1.2.3 [CHANGELOG.md]
set -euo pipefail

version="${1:?usage: release-notes.sh <version> [changelog]}"
changelog="${2:-CHANGELOG.md}"

awk -v heading="## [${version}]" '
  index($0, heading) == 1 { found = 1; next }
  # The next version heading, or the link-reference block at the end of the
  # file, both mean this section is over.
  found && index($0, "## [") == 1 { exit }
  found && index($0, "[Unreleased]:") == 1 { exit }
  found { print }
' "$changelog" | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
