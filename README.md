# DevNest

A single-binary command line toolkit for the routine work that surrounds writing code.

Which process is holding port 5173? How much disk is `node_modules` eating across every project on
this machine? Did anyone commit an API key last week? What is the SHA-256 of this artifact? What
is actually inside this JWT?

All of that is solved somewhere, with a different command on each operating system, a personal
collection of installed utilities, and a browser tab for the things that are inconvenient locally.
DevNest collects it into one executable with one consistent interface that behaves identically on
Windows, Linux, and macOS.

> **Status: 0.1.0, the first release.** Every command group works end to end on Windows, Linux, and
> macOS: `file`, `network`, `security`, `log`, `env`, `scan`, `encode`, `decode`, `json`, `yaml`,
> `port`, `clean`, `git`, `secret`, `config`, `doctor`, `export`, and `completion`.
>
> Pre-1.0, so command names, flag names, JSON field names, and exit codes can still change. The
> compatibility promise starts at 1.0. See [docs/roadmap.md](docs/roadmap.md).

## What works today

```
devnest file organize ~/Downloads      group files into Images/jpg, Documents/pdf, ...
devnest file duplicate ~/Pictures      find identical files by content, not by name
devnest file rename ./photos           batch rename, with a preview and a rollback record
devnest file filter . --extension pdf  search by extension, category, name, or size
devnest file size C:\projects          where the disk space actually went
devnest file hash installer.exe        SHA-256, SHA-512, MD5

devnest network monitor example.com    is it up, how fast, what status
devnest network http example.com       one request, full timing and header detail
devnest network latency example.com    min, average, median, max over several attempts
devnest network ping example.com       is the host reachable (TCP probe)
devnest network dns example.com        A, AAAA, CNAME, MX, TXT, NS
devnest network ssl example.com        issuer, expiry, days left, trust status
devnest network scan example.com       which ports are open, what they are probably for

devnest security password              a strong password from the system random source
devnest security password-check --stdin  score a password and say what is wrong with it
devnest security hash 'text'           SHA-256, SHA-512, MD5 over text, a file, or a pipe
devnest security checksum file.zip <hash>  verify a download against its published digest
devnest security checksum --check SHA256SUMS  verify a directory of downloads against a checksum file
devnest security encode / decode       Base64, both alphabets

devnest log analyze access.log         size, line count, detected format, read time
devnest log http access.log            requests, methods, statuses, busiest endpoints
devnest log errors app.log             failures grouped and counted, with line numbers
devnest log status access.log          how much of the traffic failed, and with what
devnest log top access.log             the endpoints that were asked for most
devnest log search app.log timeout     every matching line, with its line number
devnest log stats app.log              line lengths, and which lines are enormous

devnest env                            os, shell, detected toolchains, PATH health
devnest env which python --versions    every python on PATH and what each one reports
devnest env path --shadows             executables that exist in more than one place
devnest env vars GO                    environment variables, credentials hidden
devnest scan                           what a project is made of, ignoring node_modules
devnest scan lines                     code, comment, and blank lines by language
devnest scan tree --depth 2            the directory shape, with totals per branch

devnest encode hex / url               hex and percent-encoding, both directions
devnest decode jwt --stdin             a token's header, claims, and expiry, offline
devnest json package.json              valid or not, and the line and column if not
devnest json format api.json           one indentation width, key order untouched
devnest json query api.json users[0]   select a subtree with a path expression
devnest json to-yaml / to-csv          convert, with nested values reported not forced
devnest yaml to-json manifest.yaml     multi-document YAML becomes a JSON array

devnest port list                      what is listening, and which process holds it
devnest port check 3000                is the port free? exit 3 says it is not
devnest port free 3000                 ask the process to exit, after naming it
devnest clean .                        what a build could regenerate, and its size
devnest clean apply . --pattern dist   remove one kind of directory, after confirming
devnest clean rules                    everything clean would ever consider removing

devnest git                            branch, remotes, counts, how idle the history is
devnest git stale --print-commands     quiet branches, with the commands to remove them
devnest git large                      what is making the repository slow to clone
devnest secret scan --fail-on high     credentials in the tree, as a CI gate
devnest secret scan --baseline b.json  accept what an old repository already has, gate on new
devnest secret history --all           credentials committed at any point, still leaked
devnest secret rules                   every detector, its severity, its entropy floor
```

Nothing in the file group deletes a file, and nothing that changes the disk does so without
`--apply`. The network and security commands exit non-zero when the answer is negative: the site
is down, the host is unreachable, the certificate has expired, or the checksum does not
match. They drop straight into cron or CI.

A password given to `password-check` is never stored, never logged, and never appears in the
output. The security module has no logger at all, which is the surest way to keep it that way.

## Installing

```bash
brew tap zeeqsleepy/devnest
brew install --cask devnest
```

Or download the archive for your platform from the
[releases page](https://github.com/zeeqsleepy/DevNest/releases); on Linux there is a `.deb` and an
`.rpm`. Extract, put the binary on PATH, done. Checksum verification and shell completion setup are
in [docs/installation.md](docs/installation.md).

## What is still to come

| | |
|---|---|
| winget | The manifest is generated with every release; submitting it is the missing step |
| `devnest init` | Project scaffolding from templates, deliberately deferred past 1.0 |
| 1.0 | The compatibility promise, once the surface has been used enough to freeze it |

Full surface in [docs/commands.md](docs/commands.md); the plan in
[docs/roadmap.md](docs/roadmap.md).

## Design commitments

**One binary, no runtime.** Static Go executable. Download, put on PATH, done. No interpreter, no
shared libraries, no installer. That matters on a locked-down corporate machine where a
package manager is not an option.

**Identical on every platform.** Where the operating systems differ, the difference is absorbed
inside DevNest. `devnest port list` returns the same fields on Windows and Linux. Windows is the
primary development platform, not a port done afterwards.

**Every command speaks JSON.** `--output json` on anything, with stable field names and meaningful
exit codes. The table view and the JSON view are two renderings of the same data: the JSON never
contains less.

**Nothing leaves your machine unasked.** Only the `devnest network` group opens a socket, and only
to the address you named. No telemetry, no analytics, no update check, not even opt-in. Every
network operation is bounded by a timeout, credentials are dropped on a cross-origin redirect, and
credential-shaped headers are masked in output by default.

**Safe by default.** Nothing destructive happens without an explicit flag. `devnest file organize`
shows what it would move and stops; moving requires `--apply`, and even then an existing file is
never replaced.

Two commands can destroy something, and both are built around that. `devnest clean` deletes only
directories whose names are in a published rule table, needs a project file beside a generic name
such as `build`, re-checks every candidate in the moment before removing it, and does nothing at
all without `--apply`. `devnest port free` names the process, asks, and requests an exit rather
than a kill unless you pass `--force`.

## Documentation

**Getting started**: [installation](docs/installation.md) ·
[quick start](docs/quick-start.md) · [commands](docs/commands.md) ·
[CLI reference](docs/cli-reference.md) · [configuration](docs/configuration.md) ·
[troubleshooting](docs/troubleshooting.md) · [FAQ](docs/faq.md)

**Design and architecture**: [product requirements](docs/prd.md) ·
[architecture](docs/architecture.md) · [design philosophy](docs/design.md) ·
[modules](docs/modules.md) · [schema](docs/schema.md) · [flow](docs/flow.md) ·
[folder structure](docs/folder-structure.md) · [internal interfaces](docs/api.md) ·
[roadmap](docs/roadmap.md)

**Contributing**: [development guide](docs/development-guide.md) ·
[contributing guide](docs/contributing-guide.md) · [project rules](docs/rules.md) ·
[coding standard](docs/coding-standard.md) · [testing](docs/testing.md) ·
[error handling](docs/error-handling.md) · [logging](docs/logging.md) ·
[performance](docs/performance.md) · [security](docs/security.md) ·
[export system](docs/export-system.md) · [release process](docs/release-process.md)

## Building from source

Requires Go 1.25 or newer. One dependency, a YAML parser, fetched by the build.

```
git clone https://github.com/zeeqsleepy/DevNest.git
cd DevNest
make build
./dist/devnest version
```

`make check` runs the linter and the full test suite, the same thing CI runs.

## Contributing

Contributions are welcome, particularly Windows fixes, new toolchain detections, and new artifact
patterns for `clean`.

Open an issue before writing a feature. The project stays useful by staying narrow, and there is a
list of things it deliberately will not do in [docs/prd.md](docs/prd.md). Finding that out after a
week of work is a bad experience for everyone.

Start with [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

To report a vulnerability, use the private channel described in [SECURITY.md](SECURITY.md). Do not
open a public issue.

## License

[MIT](LICENSE).

Use it, change it, redistribute it, sell it, put it in a paid product. Keep the copyright notice
and there are no other conditions.

Releases up to and including 0.1.0 went out under the Apache License 2.0 with the Commons Clause,
which withheld the right to sell. That restriction bought nothing and cost the project Homebrew
core, the Linux distribution repositories, and anyone who will only contribute to something open
source. A copy taken under the old terms keeps them; everything from here is MIT.
