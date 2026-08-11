# Performance

Status: every target below has a benchmark behind it as of 1.0.0; the measured figures and the
committed baseline are in `benchmarks/baseline.txt`
Last revised: 2026-07-25

Speed is a feature here, not a nicety. A utility that answers a trivial question in two seconds
stops being used, whatever its capabilities. The targets below are the difference between a tool
that becomes a habit and one that gets uninstalled.

## Targets

Written against a mid-range 2023 laptop (8 cores, NVMe storage, 16 GB RAM) as a reference machine,
not a minimum requirement. The measured figures below come from a different machine, recorded in
`benchmarks/baseline.txt`, so the two are close but not directly comparable.

| Operation | Target | Ceiling |
|---|---|---|
| Cold start to first output (`--version`) | 30 ms | 50 ms |
| `env` full toolchain detection | 300 ms | 800 ms |
| `scan` structural summary over 10,000 files | 500 ms | 1.5 s |
| `scan` over 10,000 files | 500 ms | 1.5 s |
| `scan` over 100,000 files | 4 s | 10 s |
| `clean` enumeration over 10,000 files | 400 ms | 1 s |
| `port list` | 100 ms | 300 ms |
| `hash` of a 1 GB file | 2 s | 4 s |
| `secret scan` over 10,000 files | 3 s | 8 s |
| `json format` of a 10 MB document | 300 ms | 800 ms |
| `log analyze` of a 100 MB log | 500 ms | 1.5 s |
| `log http` of a 100 MB access log | 3 s | 6 s |

Ceilings are the point at which a regression is treated as a defect rather than as tuning.

**Memory:** under 50 MB for typical operations, and never proportional to input size for anything
streamable. Hashing a 10 GB file must not allocate 10 GB.

**Binary size:** under 25 MB uncompressed for the release build.

## Startup

Startup is the number people feel most, because it is paid on every invocation including the ones
that do almost nothing.

What happens before the command handler runs is deliberately tiny: build version metadata, create
a context, install a signal handler, declare the command tree, parse arguments, resolve
configuration, construct a logger, select a renderer. None of that touches the disk except the
config file read.

Rules that keep it that way:

- **No `init()` doing work.** Package initialisation runs on every invocation regardless of which
  command was asked for.
- **Nothing eager.** Secret detection rules, toolchain detection tables, and templates load inside
  the command that needs them, not at startup.
- **No filesystem walking, no network, no process enumeration before the handler.**
- **Command tree construction is declarative.** Building the tree allocates structs; it does not
  execute anything.
- **The config read is one file, parsed once.** No directory search up the tree, no merging of
  multiple discovered files.

A startup benchmark runs in CI. It is the one performance number worth watching per-commit,
because startup cost creeps in one import at a time and nobody notices until it is 200 ms.

### Measured

| Benchmark | Time | Allocations | Target |
|---|---|---|---|
| `devnest version`, whole process | 10.1 ms | 274 | 30 ms |

That figure is the process: fork, load, resolve configuration, render, exit. Roughly two thirds of
it is Windows starting a process at all, which is why the number is measured by running the binary
rather than by calling a function.

## Filesystem work

Four modules walk trees, and one shared walker in `platform/fs` does it for all of them.

- **One `stat` per entry.** Directory reads already return most of what is needed on both Windows
  and Linux; re-statting every entry roughly doubles the syscall count for no benefit.
- **Ignore rules applied at the directory level.** Skipping `node_modules` before descending is
  the single largest win available in this workload. Applying the rule per-file after walking into
  it is the most common way to make a scanner slow.
- **Bounded parallelism.** Directory reads run on a worker pool sized from `runtime.NumCPU()`.
  Unbounded goroutines over a deep tree exhaust file handles, which fails in a way that looks like
  a permissions bug.
- **Buffered reads with a reused buffer** for anything that reads file contents, so hashing and
  scanning do not allocate per file.
- **Streaming, never slurping.** Large files are processed through a fixed buffer. Memory use is
  independent of file size, always.
- **Results sorted once at the end.** Determinism is required for diffable reports; sorting during
  the walk costs more and buys nothing.

### Measured

Over a generated 10,000 file project tree: source files in forty directories, a `node_modules` with
a thousand files, and build output under `dist`.

| Benchmark | Time | Allocations | Target |
|---|---|---|---|
| `clean` enumeration | 10.4 ms | 42,241 | 400 ms |
| `scan` structural summary | 176 ms | 68,680 | 500 ms |
| `scan types` | 177 ms | 51,720 | 500 ms |
| `scan lines` | 4.65 s | 77,181 | - |
| `secret scan` | 4.53 s | 161,841 | 3 s |
| *standard library walk and read, no DevNest* | 5.53 s | 62,592 | - |

The last row is the point of the table. The three fast operations never open a file; the two slow
ones read every file they do not skip, and both are **faster than a plain `filepath.WalkDir` that
opens and reads the same tree**. Per-file open cost on this machine is around 400 microseconds with
Defender's real-time protection enabled, and no amount of work inside DevNest moves it. The
`secret scan` target of 3 seconds was written before that was measured; the honest reading is that
it is a target for a machine whose filesystem is not doing that, and the baseline row is what a
regression should be judged against.

Measuring this is what found a real defect: `secret scan` allocated a fresh 64 KiB line buffer for
every file, 579 MB over ten thousand of them. One buffer for the whole walk took it to 22 MB.

### Digests

Over a 64 MB file. The gigabyte target scales from the throughput rather than being measured
directly, because a benchmark should not ask a developer's disk for a gigabyte of scratch space.

| Benchmark | Throughput | Allocations | 1 GB implies | Target |
|---|---|---|---|---|
| SHA-256 | 1,620 MB/s | 86 | 0.6 s | 2 s |
| SHA-256 + SHA-512 + MD5, one pass | 350 MB/s | 95 | 2.9 s | - |

Three digests cost one read and three hash computations, which is what the shared digest helper
exists to provide. Allocation is flat in both: the file is streamed through a fixed buffer, so a
10 GB file allocates what a 64 MB one does.

## Reading a log file

The log module is the one place where the input is expected to be larger than memory, so its rules
are stricter than the general ones above and are worth stating separately.

- **One pass, always.** Seven commands, one scan each. The three HTTP commands are projections of a
  single collection rather than three reads of the same file.
- **One buffer, reused.** A line is a slice into the read buffer and is valid until the next line.
  Anything kept is copied at the point where the decision to keep it is made, which is a handful of
  places rather than every line.
- **A long line is assembled into a second reused buffer**, capped at one megabyte. Past the cap
  the content is cut and the line is marked, because a single pathological line must not be able to
  make memory follow the input.
- **Cancellation is checked every 4,096 lines.** Per line was measurable; per few thousand is not,
  and still returns within milliseconds.
- **Counters hold pointers, not values.** `m[string(b)]` as a lookup does not copy the key, but
  assigning back into the map does, so incrementing in place is the difference between one
  allocation per distinct value and one per line.
- **Cardinality is capped** at 100,000 distinct values per counter, with the overflow counted in one
  bucket and reported. Without it, a log with a million distinct URLs would put a million strings
  in a map, and the one thing that grows with the input would grow without limit.
- **Rankings are bounded**, so a result never holds more than the reported top entries plus the
  counters that produced them.

### Measured

`make bench`, on an AMD Ryzen 5 5500, Windows 11, over a generated 200,000 line combined-format
access log of about 20 MB. Throughput is bytes of log per second, which is the number that matters
for work dominated by reading the file.

| Benchmark | Time | Throughput | Allocations |
|---|---|---|---|
| `log analyze` | 7.1 ms | 2,946 MB/s | 79 |
| `log stats` | 7.6 ms | 2,747 MB/s | 114 |
| `log search` | 16.8 ms | 1,243 MB/s | 181 |
| `log errors` | 41.8 ms | 501 MB/s | 2,165 |
| `log http` | 56.6 ms | 370 MB/s | 1,273 |

A 100 MB log is five times this file, so `log analyze` implies 34 ms and `log http` 4.7 s against
targets of 500 ms and 3 s. The first has room to spare; the second is the one to watch.

The allocation counts are per run over the whole file, not per line, and that is the point: they do
not move when the file gets larger, only when it holds more distinct values. `log http` was 800,141
allocations before the counters held pointers, which is one measurement that paid for itself.

The spread across the five is all parsing. `analyze` looks at the head of the file and counts lines;
`http` parses every field of every line and updates five counters.

## Detecting toolchains

`env` is dominated by starting processes, and the two things that keep it inside its budget are
both about doing less of that.

- **Locate before running.** A tool is looked up on PATH first and only run if it is there. On a
  typical machine that skips most of the thirty-entry table without creating a single process.
- **Probe in a bounded pool.** The tools that are present are probed concurrently, four to eight at
  a time. In sequence the command costs the sum of every version flag; the pool costs the slowest
  few. It is bounded rather than one goroutine per tool because thirty simultaneous process
  creations is a burst the machine feels and the saving past a handful is nothing.
- **Every probe has a timeout.** A version flag that opens a socket or waits on a lock cannot hold
  the whole summary hostage. A tool slower than the timeout is reported as not answering.

## Concurrency

Parallelism is applied where it measurably helps and nowhere else. Currently that means directory
walking, multi-file hashing, and probing toolchains in `env`.

- Pool size defaults to `runtime.NumCPU()` and is configurable, because I/O-bound workloads on
  network storage want a different number from CPU-bound ones on NVMe.
- Every worker owns its data and sends results over a channel. No shared mutable state means no
  locks in the hot path.
- Cancellation is checked at loop boundaries, so an interrupt during a large scan returns
  promptly.
- Any concurrent block carries a comment stating the measured benefit. Concurrency added on
  intuition usually costs more in coordination than it saves.

## Allocation

- **Preallocate when the size is known.** `make([]T, 0, n)` when `n` is available.
- **Reuse buffers** through `sync.Pool` in hot paths only, after measurement. A pool in a cold
  path is complexity with no return.
- **Avoid string concatenation in loops.** `strings.Builder` with a preallocated capacity.
- **Do not convert between `[]byte` and `string` casually.** Each conversion copies, and in a
  per-file path that adds up quickly.
- **Return values, not pointers,** for small structs. Pointer-chasing costs more than copying 32
  bytes, and heap allocation costs more than both.

## Documents

`data` is the module that holds its input in memory and says so, with a 64 MiB limit. What that
costs is worth stating rather than implying.

### Measured

Over a generated 10 MB JSON document: an array of objects with nested fields, which is the shape an
API response has.

| Benchmark | Time | Throughput | Allocations | Target |
|---|---|---|---|---|
| `json format` | 222 ms | 47 MB/s | 2,396,796 | 300 ms |
| `json minify` | 193 ms | 54 MB/s | 2,396,793 | - |
| `json query` | 145 ms | 72 MB/s | 2,396,797 | - |

Inside the target, and the allocation figure is the honest part: 2.4 million allocations and 211 MB
of transient memory to reprint 10 MB. That is `encoding/json` decoding into `any`, which allocates
per value. It is a known cost of the approach rather than a defect, and the size limit is what keeps
it bounded. A streaming reprinter would fix it and is not worth writing until somebody formats a
document large enough to care.

## Output

Rendering is not free at scale: a table with 50,000 rows can take longer to format than the scan
that produced it.

- Writers are buffered, flushed once at the end.
- Column widths are computed in one pass, not by re-scanning per column.
- JSON encodes through a streaming encoder, not by building a document in memory first.
- Progress indicators update at most 10 times per second, on stderr, only when stdout is a
  terminal. A progress bar redrawing per file is a measurable cost and, redirected, is also
  megabytes of escape sequences.

## Measurement

**No optimisation without a benchmark.** A change justified by "this should be faster" that has no
before-and-after number does not get merged. Intuition about Go performance is wrong often enough
that the rule pays for itself.

Benchmarks live in `benchmarks/` with committed baselines annotated with the hardware they were
measured on: `benchmarks/baseline.txt` is the current one, regenerated wholesale with
`go test -run='^$' -bench=. -benchmem -count=1 ./benchmarks/` rather than edited by hand. Profiling uses the standard toolchain: `pprof` for CPU and memory, `-benchmem` for
allocation counts, and the execution tracer when a problem looks like contention rather than
throughput.

**In CI:** benchmarks run on a schedule rather than per-push, because shared runners are too noisy
for per-commit numbers to mean anything. A regression beyond 20% against the baseline opens an
issue. It does not fail a build: a benchmark that fails builds noisily is a benchmark everyone
learns to ignore.

The startup benchmark is the exception and runs on every push, because it is fast, stable enough
to be meaningful, and the metric most likely to degrade unnoticed.

## Deliberate trade-offs

Some things are slower than they could be, on purpose:

- **Deterministic ordering** costs a sort. Diffable output is worth more than the milliseconds.
- **Full path resolution before every security decision** costs syscalls. Not doing it is a
  time-of-check/time-of-use hole; see `security.md`.
- **Enumerate-then-act in destructive commands** walks the tree twice. Mutating during a walk is
  faster and unreasonable.
- **Redaction in the secret scanner** costs a pass over each match. Non-negotiable.
- **Bounded worker pools** are slower than unbounded goroutines on small inputs. They are also the
  reason large inputs do not exhaust file handles.
- **The log commands read the whole file even when the output is capped.** `log search --limit 20`
  could stop at the twentieth match and be much faster on a large file. It does not, because then
  the reported match count would be the limit rather than the truth, and a number that is sometimes
  the answer and sometimes an artefact of a flag is worse than a slower command.

Each of these is a case where correctness or safety beat speed. They are recorded here so nobody
"optimises" one of them later without knowing what it was for.
