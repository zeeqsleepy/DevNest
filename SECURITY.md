# Security Policy

## Supported versions

| Version | Supported |
|---|---|
| Latest minor release | Yes |
| Previous minor release | Security fixes only |
| Anything older | No |

Pre-1.0 releases carry no support commitment.

## Reporting a vulnerability

**Do not open a public issue.**

Use GitHub's private vulnerability reporting on this repository: the "Report a vulnerability"
button under the Security tab. If that is unavailable to you, contact a maintainer directly
through the address listed on the repository's organisation profile.

### What to include

- What the issue is and why it is a security problem.
- Steps to reproduce, or a proof of concept.
- Affected version, operating system, and architecture.
- What an attacker could achieve, and what access they would need first.
- A suggested fix, if you have one.

### What to expect

| When | What |
|---|---|
| Within 48 hours | Acknowledgement that the report was received |
| Within 7 days | Initial assessment and a severity judgement |
| Within 30 days | A fix released, or an explanation of why it will take longer |

You will be credited in the advisory and the changelog unless you prefer otherwise.

We ask that you give us a reasonable window to release a fix before disclosing publicly. The
advisory publishes together with the release, so users can update at the moment they learn about
the problem.

## Scope

### In scope

- Path traversal: any input causing an operation to act outside its intended root.
- Destructive operations bypassing their guards: `devnest file organize` or `devnest file rename`
  moving a file outside its root or replacing an existing file, `devnest clean` removing something
  outside the scan root, or `devnest port free` terminating an unintended process.
- Credential exposure: a secret appearing in output, logs, exported reports, or an error message,
  in any format at any verbosity.
- Credentials forwarded to an unintended host by `devnest http`, particularly across a redirect.
- Command injection through a filename, a path, an argument, or a configuration value.
- Denial of service from untrusted input: a crafted repository, response, or data file causing
  unbounded memory or CPU use.
- Insecure file creation: configuration or reports written with permissions that expose their
  contents.
- Supply-chain issues in the dependency graph.
- TLS verification being skipped or weakened without the user explicitly asking.

### Out of scope

- Anything requiring the attacker to already have shell access as the user. Someone who can run
  `devnest` can already run `rm`. DevNest is not a sandbox.
- A user deliberately passing `--force` or `--insecure` past a guard that told them what would
  happen.
- Vulnerabilities in the operating system, the Go toolchain, or external tools DevNest invokes.
- Missing hardening that has no demonstrated exploit.
- Social engineering.
- Findings from automated scanners with no working proof of concept.

## Security design

The threat model and the mitigations are documented in [docs/security.md](docs/security.md).
Summary of the commitments:

- Read-only by default; destructive operations require an explicit flag and, where irreversible,
  a confirmation.
- No command deletes a file. The most destructive operation available is a rename, and a rename
  that would replace an existing file is refused.
- No network access except `devnest http`. No telemetry, no update check, no error reporting,
  not even opt-in.
- Secrets are never printed in full, in any output format, at any verbosity, including exported
  reports.
- All paths are fully resolved before any security decision is made, and every target is verified
  to be contained within the operation root.
- Subprocesses are executed by argument vector, never through a shell.
- Credentials are dropped on cross-origin redirects.
- No privilege escalation is ever requested.

## Release integrity

- Release binaries are built by CI from a tagged commit, never from a maintainer's machine.
- Builds are reproducible: the same tag produces identical bytes.
- SHA-256 checksums are published for every artifact.
- Dependencies are pinned with committed checksums, and vulnerability scanning runs on every push
  and on a schedule.

## Security fixes

A security fix ships as a patch release to every supported version, not only the latest. Work
happens in a private fork until the fix is ready, so the vulnerability is not disclosed by the
commit history before users can update.
