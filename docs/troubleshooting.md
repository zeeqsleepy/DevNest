# Troubleshooting

Status: current as of 1.0.0. Every command shown here runs; the issues themselves are
still partly anticipated rather than reported
Last revised: 2026-08-12

## Start here

```
devnest doctor
```

Checks the configuration file, config directory permissions, embedded rule sets, optional external
tools, and terminal detection. Most environmental problems show up here.

Then add `--verbose` to whatever failed. It prints the full error chain, the resolved
configuration with the origin of every value, and per-item detail from the running command.

---

## `devnest` is not recognised as a command

The binary is not on PATH, or the terminal was opened before PATH changed.

Open a **new** terminal first: a PATH change does not affect already-running shells. If it still
fails:

```powershell
# Windows
Get-Command devnest
$env:Path -split ';'
```

```bash
# macOS, Linux
which devnest
echo $PATH | tr ':' '\n'
```

If the install directory is missing from that list, add it. See `installation.md`.

---

## macOS blocks the binary

Gatekeeper. The binary is not notarised by Apple.

```bash
xattr -d com.apple.quarantine /usr/local/bin/devnest
```

Or System Settings → Privacy & Security, and allow it after the first blocked attempt.

Installing through Homebrew avoids this.

---

## Colour output is garbled, or escape sequences appear as text

The terminal does not support ANSI sequences. Common in the legacy `cmd.exe` console and in some
CI log viewers.

```
devnest scan --no-color
```

Or set `NO_COLOR=1` in the environment to disable it permanently. Or use Windows Terminal or
PowerShell 7, both of which handle it correctly.

DevNest disables colour automatically when stdout is not a terminal, so this only appears in
interactive use.

---

## Piped output contains log lines

It should not: logs go to stderr and results to stdout, always, at every verbosity level. If you
see log output in a pipe, that is a defect worth reporting.

What is more likely: `2>&1` somewhere in the pipeline is merging the streams. Remove it, or
redirect stderr separately:

```bash
devnest scan --output json 2>/dev/null | jq '.data'
```

```powershell
devnest scan --output json 2>$null | ConvertFrom-Json
```

---

## Permission denied during a scan

Normal, and not fatal. DevNest skips what it cannot read, records a warning, and continues. The
warnings appear at the end of the run and in the `warnings` array of JSON output.

To see which paths were skipped:

```
devnest scan --verbose
```

To keep them out of the terminal:

```
devnest scan --quiet
```

They are still counted in the result and still listed in the `warnings` array of `--output json`.
There is no flag that drops them entirely: a summary that quietly covered less of the tree than it
says it did is the one result nobody should be able to ask for.

To read them, run the terminal elevated. DevNest never requests elevation on its own.

---

## A scan is very slow

Usually a huge dependency directory being walked.

```
devnest scan --exclude node_modules --exclude .git
```

Check that `.gitignore` is being respected: it is by default, and `--no-ignore` turns that off.
If you passed `--no-ignore`, that is the cause.

`--follow-symlinks` on a tree with a symlink pointing somewhere large can walk far more than
expected. It is off by default for this reason.

If it is still slow with a reasonable file count, that is worth reporting with the output of
`devnest scan --verbose`, which includes timing.

---

## `clean` did not delete anything

Working as intended. Dry run is the default:

```
devnest clean --apply
```

If `--apply` also removed nothing, either nothing matched the patterns, or a `protect` entry in
your configuration is excluding it. Check:

```
devnest config
devnest clean --verbose
```

---

## `clean` refuses to run

It refuses at a filesystem root, in a home directory, and in system directories, without `--force`
plus an interactive confirmation.

If you genuinely intend it, `cd` into the specific project directory first. That is almost always
the right fix, and the guard exists because the alternative failure mode is catastrophic.

---

## `port free` cannot stop a process

Several possible causes:

- **The process belongs to another user.** Run the terminal elevated. DevNest will not request
  elevation itself.
- **It is a system process.** PID 0 and PID 1 are refused unconditionally.
- **It is not responding to a graceful stop.** Use `--force` for forceful termination after the
  timeout.
- **It already exited.** The PID is re-verified against the port immediately before signalling, so
  a process that exited between listing and acting produces a clear message rather than killing
  whatever inherited the PID.

---

## `port list` shows no process for some ports

Process ownership is not always visible without elevation, particularly for processes owned by
other users or by the system.

DevNest reports the socket with unknown ownership rather than hiding it, because a silently short
list is misleading. Run elevated to see more.

---

## A git command says git is not available

`devnest git` shells out to the `git` executable. Install git, or add it to PATH.

```
devnest env which git
```

If that finds nothing, git is not on PATH for this shell, which can differ from what your IDE
sees.

---

## The secret scanner reports things that are not secrets

Expected. The scanner reports *candidates* and the output says so; the tradeoff is set toward
catching real leaks.

To reduce noise:

```
devnest secret scan --entropy 5.0
devnest secret scan --exclude "testdata/" --exclude "*.lock"
```

Make it permanent in configuration under `[secret]`. See `configuration.md`.

Raising the floor is safe in the sense that matters: it moves only the rules that match by shape,
never the ones matching a provider's prefix, so no value of `--entropy` will stop an AWS or GitHub
key from being reported.

To check whether a specific string would match, and why:

```
devnest secret test "the-string-in-question"
```

---

## The scanner reports hundreds of things this repository has always had

That is what a baseline is for. Accept what is there today, then gate on what arrives afterwards:

```
devnest secret scan --baseline .devnest-secrets.json --update-baseline
devnest secret scan --baseline .devnest-secrets.json --fail-on high
```

Read the file before committing it. Accepting is not fixing, and anything in there that is a real
credential still needs rotating; the excerpt in the file is redacted, so the file itself is safe to
commit either way.

Later runs report `baselineStale` when entries stop matching. That is the signal to regenerate the
file with `--update-baseline`, which prunes them.

---

## The secret scanner missed something

Also expected. Pattern matching cannot catch everything, and a scanner is a safety net rather than
a guarantee.

```
devnest secret rules
```

lists the active rules, which is the whole surface of what a scan can find. There is no way to add
your own: a rule needs a name, a severity, an entropy floor, and a pattern with a capture group,
and none of that fits the configuration file's format. A credential format worth detecting is
usually worth contributing upstream, where it gets a rule entry, a floor, and a test.

---

## An HTTP request times out

The default is 30 seconds.

```
devnest network http https://slow.example.com --timeout 120s
```

If it fails immediately rather than timing out, it is DNS, a proxy, or TLS. `--verbose` shows the
timing breakdown, which identifies which stage failed.

---

## An HTTPS request fails with a certificate error

Usually a corporate TLS-inspecting proxy with a certificate not in the system trust store. The
right fix is installing that certificate in the system store: DevNest uses the system trust store
and will then work like everything else.

`--insecure` exists, requires a separate acknowledgement flag, and warns every time. Use it for
diagnosis, not as a permanent workaround.

---

## Configuration changes have no effect

Check what is actually in effect and where each value came from:

```
devnest config
```

The most common causes: a flag on the command line overriding the file, an environment variable
overriding the file, or the file being in a different location than expected.

```
devnest config path
devnest config validate
```

`--config <path>` on a file that does not exist is a fatal error rather than a silent fallback,
precisely so this class of confusion is caught immediately.

---

## Long paths fail on Windows

Paths over 260 characters need long path support enabled system-wide:

```powershell
# requires elevation
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\FileSystem" `
    -Name "LongPathsEnabled" -Value 1
```

Restart afterwards. DevNest handles long paths correctly when the operating system permits them;
without this setting the operating system itself refuses.

---

## Reporting a problem

Include:

- `devnest version`
- `devnest doctor`
- Operating system and version
- The exact command
- What you expected and what happened
- The `--verbose` output, if relevant

Redact anything sensitive before pasting. Paths often contain names.

For a security issue, do not open a public issue: use the private channel in `SECURITY.md`.
