# Configuration

Status: implemented, including the `devnest config` commands
Last revised: 2026-07-25

DevNest works fully with no configuration at all. That is the primary design constraint here: a
tool that needs setup before first use has already lost most of its potential users. Configuration
exists to remove repetition for people who use it daily, not to make it usable.

## Precedence

Four sources, lowest to highest:

```
1. compiled defaults
2. config file
3. environment variables       DEVNEST_ prefix
4. command-line flags
```

Each layer overrides only the keys it actually sets. An absent key is not the same as a zero
value: a config file that sets `general.color` does not reset every other key to its zero value.

Resolution happens once, in `internal/cli`, before any module runs. The result is immutable from
that point. No module reads configuration; whatever a module needs arrives in its `Request`,
which is what lets a module be tested without a config file existing.

In code the split is: `config.Load` handles defaults, file, and environment, and the CLI layer
applies flag overrides on the returned value before calling `Validate`. Flags are the only source
whose "was it set" question the CLI layer can answer, which is why that step lives there.

`devnest config` prints the resolved configuration **with the origin of each value**: default,
file, environment, or flag. That single feature answers most "why is it behaving like that"
questions without anyone reading this document.

## File location

Platform-conventional, because putting a dotfile in the home directory on Windows annoys people
correctly:

| Platform | Path |
|---|---|
| Windows | `%APPDATA%\devnest\config.toml` |
| Linux | `$XDG_CONFIG_HOME/devnest/config.toml`, or `~/.config/devnest/config.toml` |
| macOS | `~/Library/Application Support/devnest/config.toml` |

`--config <path>` overrides the location. A file requested explicitly that does not exist is a
fatal error: the user asked for that file, and quietly using different settings would produce
results they did not ask for. A missing *default* file is not an error; compiled defaults apply
and most users never create one.

`devnest config path` prints the file in use. `devnest config init` writes the annotated default.

## Project-local configuration

A project can carry its own settings in a `.devnest.toml` at the root of its tree. Running
DevNest from anywhere inside the project discovers it by walking up from the working directory, so
`scan` respects the exclusions the project cares about without every contributor passing the same
flags.

**Only a narrow set of keys is allowed, and the safety-relevant ones are not among them.** A file
that travels with a clone must never widen what a delete command will remove, must never turn off a
confirmation, and must never hide paths from a secret scan. Those are decisions for the machine a
command actually runs on, not for code that arrives with a repository.

Allowed keys:
- `[general]`: `output`, `color`, `verbosity`.
- `[scan]`: `follow_symlinks`, `respect_ignore`, `max_depth`, `exclude`.
- `[network]`: `timeout_ms`, `follow_redirect`, `max_redirects`, `attempts`, `interval_ms`.
- `[security]`: the three password-shape defaults.

Never allowed, and ignored with a warning when present: everything in `[clean]`, everything in
`[secret]`, and `general.confirm`. A key DevNest does not recognise at all is warned about exactly
as in the main file.

**Precedence.** The project file sits between the machine's file and the environment: default <
machine file < project file < environment < flag. The project file beats the machine's file
because it is nearer the call site; the environment and an explicit flag still win, because they
are a deliberate override for one run.

`devnest config <command>` ignores the project file: those commands edit the one file, and a
project file read into that edit would fight the caller.

## Format

TOML. Chosen over the alternatives for concrete reasons: JSON has no comments, and a config file
you cannot annotate is a config file nobody understands six months later. YAML's indentation
sensitivity and type coercion produce a class of bug that is not worth inviting into a file users
hand-edit. INI has no standard. TOML is unambiguous, comment-friendly, and boring.

### The supported subset

DevNest decodes TOML itself rather than taking a dependency, because the configuration schema is
flat: single-level sections holding strings, integers, floats, booleans, and single-line arrays of
strings. A decoder for exactly that is small, and owning it means the parse errors name the file,
the line, and what was expected.

Supported: comments (`#`, including at the end of a line), single-level section headers, basic
strings with `\n`, `\t`, `\r`, `\"` and `\\` escapes, literal strings in single quotes, integers,
floats, booleans, and single-line arrays.

Not supported, and rejected with the line number rather than partially understood: nested tables,
arrays of tables, dotted keys, multi-line strings, and datetimes. None of them appear in the schema.
If the schema ever needs one, the replacement is a real TOML library and nothing outside
`internal/config/toml.go` has to change.

A literal string is the convenient form for a Windows path, since it needs no escaping:

```toml
[clean]
protect = ['C:\projects\keep-this']
```

## Keys

The full annotated default lives in `configs/config.example.toml`, and a test loads it on every
run to confirm it still parses and still matches the compiled defaults. An example that has drifted
teaches the wrong thing to exactly the people least able to notice.

Every section has a consumer: `[general]`, `[scan]`, `[security]`, `[network]`, `[clean]`,
`[secret]`, and `[export]`.

`export.directory` takes effect when `--export` is given a bare filename, which then lands there
instead of in the working directory; a path with a directory in it is used exactly as typed,
because somebody who typed a location meant it. `export.timestamp_files` inserts the moment before
the extension, so a command run twice leaves two reports rather than one overwritten one.

`secret.entropy_threshold` sets the floor for the scanning rules that match by shape, and `--entropy`
overrides it for one run. Neither touches the rules that match a provider's prefix: raising the
floor is for quietening guesses, and a setting that could hide an AWS key would be a switch for
turning the scanner off rather than a tuning knob. It defaults to zero, meaning every rule keeps
the floor it was written with, because a compiled default stricter than the rules themselves would
drop candidates on a machine whose owner never configured anything.

There is no key for user-supplied scanning rules, and there will not be one in this file: a rule
needs a name, a severity, an entropy floor, and a capture group, none of which fit a format that
holds flat sections of scalars and string lists. `secret.custom_rules` existed and was read by
nothing; it has been removed. See `modules.md`.

`clean.patterns` adds directory names to the built-in rule set rather than replacing it, and a
name added there still needs a project marker beside it before it counts, exactly like the
built-in generic names. `clean.protect` lists paths that are never removed whatever matches.
Neither can lift the refusal to run at a filesystem root or in a home directory: that guard
answers only to `--force` on the command line, because a safety rule that a file can switch off
is not a safety rule.

`[security]` describes the shape of a generated password, its length, whether symbols are used,
whether easily misread characters are dropped. It contains no secret and never will. If a feature
ever appears to need one stored here, that is a design discussion before it is a config key; see
"What never goes in configuration" below.

`password_symbols` sets a default that either `--symbols` or `--no-symbols` overrides. Both
directions get a flag deliberately: offering only the negative would leave someone whose
configuration disables symbols with no way to ask for them once.

The `[network]` section is named for the whole layer rather than for HTTP (it was `[http]` in
Phase 1) because its timeout bounds a DNS lookup and a TLS handshake as well as a request. A key
called `http.timeout_ms` governing how long a name resolution may take would be a small lie in a
file people hand-edit. The rename happened before 0.1.0, so it cost nobody anything; it is
recorded in the changelog.

```toml
[general]
output          = "table"      # table | json | csv | markdown
color           = "auto"       # auto | always | never
verbosity       = "warn"       # error | warn | info | debug
confirm         = true         # prompt before destructive operations

[scan]
follow_symlinks = false
respect_ignore  = true
max_depth       = 0            # 0 = unlimited
exclude         = [".git", "node_modules"]

[clean]
patterns        = ["node_modules", "dist", "build", "target", "__pycache__"]
protect         = []           # never touched, whatever the patterns match
require_confirm = true

[secret]
entropy_threshold = 0          # zero leaves every rule on its own floor
exclude_paths     = ["testdata/", "fixtures/", "*.lock"]

[security]
password_length            = 20
password_symbols           = true
password_exclude_ambiguous = false

[network]
timeout_ms      = 30000
follow_redirect = true
max_redirects   = 10
attempts        = 3            # default for latency and ping
interval_ms     = 200          # pause between attempts

[export]
directory       = "reports"
timestamp_files = true
```

Naming: sections match command groups, keys are `snake_case`, booleans are positive assertions
(`follow_symlinks`, not `no_symlinks`), and anything with a unit carries it in the name
(`timeout_ms`).

## Environment variables

Mirror the file structure with a `DEVNEST_` prefix, section, and key, uppercased:

```
DEVNEST_GENERAL_OUTPUT=json
DEVNEST_SCAN_MAX_DEPTH=5
DEVNEST_CLEAN_REQUIRE_CONFIRM=false
DEVNEST_NETWORK_TIMEOUT_MS=5000
```

List values are comma-separated: `DEVNEST_SCAN_EXCLUDE=.git,node_modules,vendor`.

Environment variables are the right layer for CI: a workflow sets `DEVNEST_GENERAL_OUTPUT=json`
once and every step gets machine-readable output without repeating a flag.

`NO_COLOR` is honoured regardless of prefix, per the informal cross-tool convention. Any value,
including an empty one, disables colour.

## Validation

Applied to the merged result, not per layer, so a value that is only valid in combination with
another is checked correctly.

- **Unknown keys**: a warning, never fatal. A newer config file read by an older binary should
  still work. The warning names the key so a typo is still caught.
- **Type mismatch**: fatal, naming the key path, the expected type, and what was found.
- **Out-of-range values**: fatal, naming the valid range.
- **Invalid enum values**: fatal, listing the valid options. `output = "tabel"` produces a
  message that includes `table`, which is faster than a documentation lookup.

Every error names the file and the line. "Invalid configuration" alone is not an acceptable
message.

A malformed environment variable is fatal on the same terms, and the message names the variable:
`DEVNEST_SCAN_MAX_DEPTH expects an integer, found "deep"`.

`devnest config validate` checks the file without running anything else, which is the right thing
to put in a setup script.

## Managing configuration

Every command below writes to, or reads from, the file resolved by `--config` or the default
location.

```
devnest config                    resolved values, with the origin of each
devnest config list               all keys and current values
devnest config get <key>          one value
devnest config set <key> <value>  write to the user config file
devnest config unset <key>        remove, reverting to the default
devnest config path               the file in use
devnest config init               write an annotated default file
devnest config validate           check for errors
```

`config set` preserves comments and key order in the existing file. A config manager that rewrites
a hand-annotated file into machine-normalised form destroys the comments the user wrote, and they
do not get them back.

Writes are atomic (temporary file in the same directory, then rename) so an interrupted write
never leaves a truncated config file that fails to parse on next run.

## What never goes in configuration

**Secrets.** DevNest holds no credentials and stores none. If a future feature appears to need
one, that is a design discussion before it is a config key.

**Anything that changes safety behaviour irreversibly.** `clean.require_confirm = false` is
allowed because someone running unattended cleanups needs it, but the root, home, and system
directory guards cannot be disabled from configuration at all; those require `--force` on the
command line, every time, visible at the call site. A safety guard that can be disabled once in a
file and then forgotten is not a safety guard.

**Aliases and custom commands.** Shells already do this well, and command names that vary per
machine make shared documentation and shared scripts unreliable.
