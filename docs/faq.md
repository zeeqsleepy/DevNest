# FAQ

Status: current as of Phase 10
Last revised: 2026-07-24

## About the project

**What is DevNest?**

A single-binary command line toolkit for the routine work that surrounds writing code: checking
what is holding a port, finding what is eating disk space, hashing a file, decoding a token,
scanning for leaked credentials, inspecting a repository. One tool, one interface, identical
behaviour on Windows, Linux, and macOS.

**Why does this need to exist? All of it is already possible.**

Possible, yes: with a different command on each platform, a personal collection of installed
utilities, and a browser tab for the things that are inconvenient locally. DevNest's contribution
is not capability, it is consolidation: one interface, one install, the same output shape
everywhere, and machine-readable output on every command.

**Is it a replacement for `jq` / `curl` / `git`?**

No. Those tools are better at their jobs than DevNest will ever be, and DevNest is not trying to
displace them. It covers the common cases well enough that you do not need to install five things
on a new machine, and it makes those cases consistent across platforms. When you need the full
power of a specialist tool, use the specialist tool.

**Why Go?**

Single static binary with no runtime dependency, genuine cross-compilation to six targets from one
machine, a standard library that covers most of what this tool needs, and fast startup. For a CLI
distributed to machines where you cannot assume a package manager exists, it is close to ideal.

**Why is Windows the primary platform?**

Because "Windows support" added later is usually broken. Developer tools written on Linux and
ported afterwards accumulate assumptions (path separators, case sensitivity, the 260-character
limit, process APIs) that are painful to unwind. Building Windows-first means those assumptions
never get made. Linux and macOS are fully supported and covered in CI equally.

**Is it open source?**

Source-available, not open source, and the distinction is worth being straight about. DevNest is
under the Apache License 2.0 with the Commons Clause: Apache 2.0 in full, patent grant included,
with one right removed. That right is selling the software, meaning providing DevNest to others for
a fee as a product or service whose value comes substantially from what DevNest does. Repackaging
it and charging for it, hosting it as a paid service, and charging for support of it are all out.

Everything else is permitted, including the case people usually worry about: using DevNest at work,
in a company that makes money, in a paid product's build pipeline. It is the tool being sold that
is restricted, not the work you do with it.

That single restriction is what stops it being open source under the OSI definition, which does not
allow a licence to restrict a field of use. Calling it open source anyway would be a lie that costs
nothing to avoid. See `LICENSE`.

## Using it

**Do I need to configure anything?**

No. Every command works immediately after install. Configuration exists to remove repetition for
daily users, not to make the tool usable.

**Does it send anything anywhere?**

Only when you run a `devnest network` command, which is the group whose entire purpose is sending
a request.
No telemetry, no analytics, no error reporting, no update check, not even opt-in. The absence of
the code is a stronger guarantee than a default setting.

**Can it delete my files?**

`devnest clean` can, and only with `--apply`. Without it, the command reports what it would remove
and stops. It only ever considers known artifact patterns, never crosses a filesystem boundary,
never touches anything outside the directory you ran it in, and refuses to run at a filesystem
root or home directory without `--force` and a confirmation. Every removal is logged with its full
path before it happens.

**Can I use it in CI?**

That is a primary use case. Every command has `--output json` with stable field names, meaningful
exit codes, and clean stdout/stderr separation. `devnest secret scan` and `devnest security checksum`
exit non-zero on a negative result specifically so they work as gates without output parsing.

**Does it need admin rights?**

No, and it never asks for them. Some information (process ownership for other users' processes)
is only visible when elevated. DevNest reports what it can see, marks the rest unknown, and
continues.

**Can I use it as a library?**

Not yet. Everything is under `internal/`, which the Go compiler prevents external modules from
importing. That is deliberate: a public API is a promise, and promises are expensive to keep. The
domain layer is already shaped for it, so if real demand appears, promoting a stable subset to
`pkg/` is a decision rather than a rewrite.

## Design questions

**Why is JSON output not automatic when piping?**

Because it would break every script written against the table output the first time someone ran
the command interactively while developing it. Colour is auto-detected; format never is. If you
want JSON, ask for it: with a flag, or with `DEVNEST_GENERAL_OUTPUT=json` in a CI environment.

**Why no plugin system?**

Third-party code loaded into this process is a security surface and a permanent compatibility
obligation. Extension happens two ways: contribute a module upstream, or consume the JSON output
from your own tool. If it is ever reconsidered, the mechanism will be a subprocess exchanging JSON
over stdio, not code loaded into DevNest.

**Why no project-local config file?**

A config file discovered by walking up from the working directory means the same command behaves
differently in different directories. Confusing when it works, dangerous when the command deletes
things. Project-specific behaviour goes in flags, which are visible at the call site. This may be
revisited for a narrow set of non-safety keys if usage demands it; see `roadmap.md`.

**Why does `devnest git` shell out instead of using a git library?**

Any machine with a repository to inspect has git installed. The CLI is a stable, documented,
extremely well-tested interface. A git implementation is an enormous surface to carry for
read-only reporting. If git is absent, the command says so clearly.

**Why does JWT decoding not verify signatures?**

Verification requires key material and key management, and doing it halfway gives a false sense of
assurance. The command exists so nobody has to paste a token into a web page to see what is in it,
and its output states plainly that the token is unverified.

**Why no emoji in the output?**

They render inconsistently across Windows terminals, break column alignment, and carry no
information a word does not carry better.

**Why are abbreviations banned in command names?**

`devnest cfg ls` saves four keystrokes and costs a documentation lookup every time someone is
unsure which abbreviation this particular tool chose. Shell history and tab completion make the
character count irrelevant.

## Contributing

**How do I add a new toolchain to `env` detection?**

A table entry: executable name, version flag, and a pattern to extract the version. No new logic.
This is the most contribution-friendly part of the project.

**Can I add a dependency?**

With a written justification covering what it does, why the standard library is insufficient, its
maintenance status, its own dependency count, its license, and what happens if it is abandoned.
See `rules.md` R13. The bar is high because each dependency is a supply-chain surface and a
permanent upgrade obligation.

**Why did my pull request get closed on scope grounds?**

Probably something in the non-goals list in `prd.md`. That list exists so the answer is
predictable rather than arbitrary: the project stays useful by staying narrow. Opening an issue
before writing code avoids this outcome.

**Can I add a command that only works on Windows?**

Only if it degrades cleanly elsewhere: reporting that it is unavailable on this platform rather
than failing obscurely. Cross-platform uniformity is a core commitment; see `design.md`.

## Practical

**Where is my configuration stored?**

| Platform | Path |
|---|---|
| Windows | `%APPDATA%\devnest\config.toml` |
| Linux | `~/.config/devnest/config.toml` |
| macOS | `~/Library/Application Support/devnest/config.toml` |

`devnest config path` prints the file in use.

**What else does it write to disk?**

Nothing, unless you ask. No cache directory, no registry keys, no service registration, no log
files. Reports go where `--export` points.

**How do I update it?**

Whatever installed it. See `installation.md`. DevNest never updates itself and never checks for
updates.

**How big is the binary?**

Under 25 MB uncompressed, and that is enforced as a release target.

**How do I report a security issue?**

Not in a public issue. Use the private channel in `SECURITY.md`.
