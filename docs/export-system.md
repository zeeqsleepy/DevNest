# Export System

Status: draft, Phase 0
Last revised: 2026-07-23

Every command can write its result to a file as well as to the terminal. This document covers how
that works and what the output looks like.

## The premise

There is exactly one result per command, and every output format is a rendering of it. There is no
separate "report generator" with its own data path, because the moment two code paths produce the
same information, they drift, and the exported report starts contradicting what the terminal
showed.

```
Result ──┬── table    → stdout
         ├── json     → stdout or file
         ├── csv      → stdout or file
         └── markdown → stdout or file
```

## Using it

```
devnest scan --export reports/scan.json
devnest scan --export reports/scan.md
devnest secret scan --export findings.csv
devnest scan --output table --export reports/scan.json
```

The format is taken from the file extension, or set explicitly with `--export-format`. Exporting
does not suppress terminal output: a user who exports usually also wants to see what happened,
and the last example shows a readable table while writing structured data for a script.

Default export directory is `reports/`, configurable under `[export]`. With
`export.timestamp_files = true`, a timestamp is inserted before the extension so repeated runs do
not overwrite each other: `scan-20260723-141502.json`.

## Formats

### JSON

The primary machine format and the one with a compatibility guarantee. Uses the envelope defined
in `schema.md`:

```json
{
  "devnest": {
    "version":   "1.0.0",
    "command":   "scan",
    "startedAt": "2026-07-23T14:15:02Z",
    "durationMs": 1834
  },
  "status": "warning",
  "data": {
    "root":        "C:\\projects\\api",
    "totalFiles":  38412,
    "totalBytes":  2469606195,
    "byExtension": [
      { "extension": ".ts",   "files": 1842, "bytes": 8934112 },
      { "extension": ".json", "files":  312, "bytes": 1204331 }
    ]
  },
  "warnings": [
    { "code": "PERMISSION_DENIED", "message": "cannot read directory",
      "path": "C:\\projects\\api\\node_modules\\.cache" }
  ],
  "error": null
}
```

Conventions, applied everywhere:

- `camelCase` field names.
- Sizes are integers in bytes, with `Bytes` in the name. Durations are integers in milliseconds,
  with `Ms` in the name. No unit-less numbers.
- No human-formatted strings. `2469606195`, never `"2.3 GB"`, formatting is a rendering concern
  and a consumer that has to parse `"2.3 GB"` back into a number has been failed by the format.
- Timestamps are RFC 3339 in UTC.
- Empty collections are `[]`, never `null`. A consumer should not need a null check before
  iterating.
- Indented by default; `--compact` for single-line.

Field names are additive within a major version. Adding a field is fine; removing or renaming one
is a breaking change under `rules.md` R50.

### CSV

For results whose data is naturally rectangular: file lists, port lists, scanner findings,
contributor statistics.

```csv
extension,files,bytes
.ts,1842,8934112
.json,312,1204331
```

- Header row always.
- RFC 4180 quoting. Fields containing a comma, quote, or newline are quoted; embedded quotes are
  doubled.
- UTF-8 with a BOM, because Excel on Windows misreads UTF-8 without one and the resulting
  mojibake report is the kind of thing that gets blamed on the tool.
- CRLF line endings, for the same reason.
- Numbers unformatted: no thousands separators, no unit suffixes. A spreadsheet can format;
  it cannot reliably unformat.

Commands whose results are deeply nested reject `--output csv` with a message naming the problem
and suggesting JSON. Flattening a tree into a rectangle silently loses structure, and a lossy
export that looks complete is worse than a refusal.

### Markdown

For pasting into a ticket, a pull request, or a wiki.

```markdown
# Scan Report: C:\projects\api

Generated 2026-07-23 14:15:02 UTC with DevNest 1.0.0

## Summary

| Metric | Value |
|---|---|
| Files | 38,412 |
| Total size | 2.3 GB |
| Directories | 4,112 |
| Duration | 1.8 s |

## By extension

| Extension | Files | Size |
|---|---|---|
| `.ts` | 1,842 | 8.5 MB |
| `.json` | 312 | 1.2 MB |

## Warnings

- Permission denied reading `node_modules\.cache`
```

This is the one format where values are formatted for reading (`2.3 GB` and `38,412`) because a
human is the only consumer. Report-shaped rather than data-shaped: headings, a summary section,
and the detail below.

Includes generation time and DevNest version, so a report pasted into a ticket six months later
still says where it came from.

## Writing

Every export follows the same sequence:

```
1. render into a temporary file in the destination directory
2. flush and fsync
3. atomic rename over the target
```

The temporary file lives in the destination directory so the rename stays within one filesystem
and therefore stays atomic. An interrupted export leaves the previous file or the new one, never a
truncated file that looks valid, which matters when the export is a scanner report someone
decides to trust.

The destination directory is created if missing. An existing target file is overwritten; a warning
is printed unless `--force` is passed. Failure to write is a fatal error, since the user asked for
a file and silently not producing one would be worse than stopping.

## Multi-command reports

`devnest export <command...>` runs several commands and writes one combined document:

```
devnest export scan clean --export reports/project-health.md
```

The combined envelope holds one entry per command under `commands`, each with its own status,
data, and warnings. Overall status is the worst of the individual statuses; overall exit code is
the worst of the individual codes.

Commands run in the order given, and a failure does not abort the rest: a partial report with a
clearly marked failed section is more useful than no report.

## Testing

- Every format is rendered from a fixed `Result` fixture and compared against a committed golden
  file, so a formatting change is visible in the diff.
- JSON output is validated against the envelope schema; a missing or renamed field fails the
  build, which is what keeps the compatibility promise honest.
- CSV output is round-tripped through a parser to confirm quoting is correct.
- The atomic write path is tested by interrupting between render and rename and asserting the
  original file is intact.
- Golden files are regenerated by a documented command, never by hand.
