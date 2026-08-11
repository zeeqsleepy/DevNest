# Quick Start

Status: current as of 1.0.0. Every command shown here runs, and a test runs each of
these examples against the built binary
Last revised: 2026-08-12

Five minutes with DevNest. Installation is in `installation.md`; this assumes `devnest` is on
PATH.

## Confirm it works

```
devnest version
```

Nothing else is needed. No configuration, no setup, no first-run wizard. Everything below works
immediately after install.

## Look at your environment

```
devnest env
```

Prints the operating system, architecture, shell, and every developer toolchain it can find with
its version and resolved path.

The one worth knowing about:

```
devnest env path
```

Flags problems with PATH: duplicate entries, entries pointing at directories that do not exist,
and **shadowed executables**, where the same command name resolves from more than one PATH entry.
That last one explains most "but I installed the new version" confusion.

```
devnest env which python
```

Shows every location `python` resolves from, in PATH order, not only the first one. That is the
information you actually want when the version is wrong.

## Look at a project

```
cd C:\projects\api
devnest scan
```

Files, directories, total size, breakdown by type, largest files. Respects `.gitignore` by
default.

```
devnest scan types --limit 20
```

The twenty biggest file types by size, which is usually where the surprise is.

```
devnest file size
```

Size by directory, largest first. The usual answer is `node_modules`, which leads to the next
command.

## Reclaim disk space

```
devnest clean
```

**This does not delete anything.** Dry run is the default. It reports what it *would* remove:
build output, dependency caches, `__pycache__`, and how much space that would reclaim.

Read the list. Then, if you agree with it:

```
devnest clean --apply
```

Every removal is logged with its full path before it happens. Nothing outside the directory you
ran it in is ever touched.

## Free a port

```
devnest port check 5173
```

Tells you whether the port is in use and which process is holding it. Exits 0 when free and 3 when
in use, so a script can branch on it without parsing anything.

```
devnest port list
```

Everything currently listening, with the owning process.

```
devnest port free 5173
```

Names the process and asks for confirmation before stopping it. Nothing happens until you agree.

## Check a file

```
devnest file hash build\app.exe
```

SHA-256 by default. `--algorithm md5` or `--all` for every algorithm in a single read of the file.

```
devnest security checksum build\app.exe 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
```

Exits 0 on a match and 1 on a mismatch, which is what makes it useful in a CI step.

For a release that published a whole checksum file rather than one digest:

```
devnest security checksum --check SHA256SUMS
```

Files listed but not downloaded are reported as missing, not failed.

## Decode something

```
devnest security decode aGVsbG8gd29ybGQ=
devnest encode hex 'hello'
devnest decode url 'a+b%26c%3Dd'
devnest decode jwt eyJhbGciOiJIUzI1NiJ9...
```

Entirely offline. Nothing is transmitted anywhere: that is the point of these commands existing.
`decode jwt` prints the header and payload and reports whether the token has expired; it does not
verify the signature and says so.

## Work with JSON

```
devnest json response.json
devnest json format response.json --indent 4
devnest json query response.json 'items[0].id'
devnest json to-csv users.json > users.csv
devnest yaml to-json docker-compose.yml
```

`devnest json <file>` validates and reports what the document holds. A parse error names the line
and the column and quotes the offending line, rather than just "invalid JSON".

Reads from stdin too:

```
devnest network http https://api.example.com/status | devnest json --stdin
```

## Send a request

```
devnest network http https://api.example.com/health
```

Status, timing breakdown (DNS, connect, TLS, first byte, total), headers, body, and certificate
expiry. Credential-shaped header values are masked by default.

## Look for leaked credentials

```
devnest secret scan
```

Scans the working tree for credential-shaped strings. Findings are always redacted: enough to
locate them, never enough to use them.

Add `--fail-on high` and it exits non-zero when something at that severity is found, which is what
makes it a pre-commit hook or a CI gate. Without the flag, finding something is still a successful
run.

A repository that already has findings starts with a baseline, so only new ones can fail a build:

```
devnest secret scan --baseline .devnest-secrets.json --update-baseline
devnest secret scan --baseline .devnest-secrets.json --fail-on high
```

## Get machine-readable output

Every command takes `--output json`:

```
devnest scan --output json
devnest port list --output json
```

The table and the JSON are two renderings of the same data; the JSON never contains less.

```
devnest scan --output json | jq '.data.totalBytes'
```

Logs go to stderr and results to stdout, always, so this works identically with `--verbose`.

## Save a report

```
devnest scan --export reports/scan.md
devnest secret scan --export findings.csv
```

Format comes from the extension. The terminal output still appears: exporting does not silence
it.

## Optional: configure it

Not required. If you find yourself passing the same flag constantly:

```
devnest config init
devnest config set general.output json
devnest config path
```

To see where every current value came from (default, file, environment, or flag):

```
devnest config
```

That single command answers most "why is it behaving like that" questions.

## Where to go next

- `commands.md`: the full command surface.
- `cli-reference.md`: flags, exit codes, piping behaviour.
- `configuration.md`: every configuration key.
- `troubleshooting.md`: when something does not work.

Or just use `--help`. Every command has one, with realistic examples.
