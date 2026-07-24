package env

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devnest/devnest/internal/platform/proc"
)

// ListRequest describes one toolchain detection run.
type ListRequest struct {
	// Only restricts detection to the named tools. Empty means the whole
	// table.
	Only []string
	// IncludeMissing keeps the tools that were not found in the result. A
	// summary wants them left out; a report answering "what is missing on
	// this build agent" wants them in.
	IncludeMissing bool
	// Timeout bounds each probe. Zero means the platform default.
	Timeout time.Duration
}

// ListResult is what was detected.
type ListResult struct {
	Tools   []Tool `json:"tools"`
	Found   int    `json:"found"`
	Missing int    `json:"missing"`
	// DurationMs is how long detection took, which is worth reporting
	// because it is dominated by tools that were slow to answer.
	DurationMs int64 `json:"durationMs"`
}

// probeWorkers bounds how many probes run at once.
//
// This is the one place in the module where concurrency is worth its
// complexity, and it is measurable: probing thirty toolchains in sequence
// costs the sum of every version flag, and the slow ones are slow in tens of
// milliseconds each. The pool is bounded rather than one goroutine per tool
// because thirty simultaneous process creations is a burst the machine feels,
// and the saving past a handful is nothing.
func probeWorkers() int {
	workers := runtime.NumCPU()
	if workers < 4 {
		return 4
	}
	if workers > 8 {
		return 8
	}
	return workers
}

// List detects installed toolchains.
//
// Each tool is looked up on PATH first, and a tool that is not there is never
// run. On a typical machine that skips most of the table without starting a
// single process, which is what keeps the whole command inside its budget.
func List(ctx context.Context, deps interface {
	Runner
	Locator
}, request ListRequest) (ListResult, error) {
	started := time.Now()

	wanted, err := selected(request.Only)
	if err != nil {
		return ListResult{}, err
	}

	tools := make([]Tool, len(wanted))
	queue := make(chan int)
	var group sync.WaitGroup

	for range probeWorkers() {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range queue {
				tools[index] = probe(ctx, deps, wanted[index], request.Timeout)
			}
		}()
	}

	for index := range wanted {
		if ctx.Err() != nil {
			break
		}
		queue <- index
	}
	close(queue)
	group.Wait()

	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}

	result := ListResult{Tools: make([]Tool, 0, len(tools))}
	for _, tool := range tools {
		if tool.Found {
			result.Found++
		} else {
			result.Missing++
			if !request.IncludeMissing {
				continue
			}
		}
		result.Tools = append(result.Tools, tool)
	}

	sortTools(result.Tools)
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

// selected resolves the requested names against the table.
//
// A name nobody has described is still detected, with no version command to
// run. Refusing it would make the tool useless for the in-house binary
// everybody on a team has and nobody has heard of.
func selected(only []string) ([]toolchain, error) {
	if len(only) == 0 {
		return toolchains(), nil
	}

	chosen := make([]toolchain, 0, len(only))
	for _, name := range only {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if known, found := findToolchain(trimmed); found {
			chosen = append(chosen, known)
			continue
		}
		chosen = append(chosen, toolchain{name: trimmed, kind: KindBuild})
	}
	return chosen, nil
}

// probe locates one tool and asks it for its version.
func probe(ctx context.Context, deps interface {
	Runner
	Locator
}, tool toolchain, timeout time.Duration) Tool {
	found := Tool{Name: tool.name, Kind: tool.kind}

	locations := deps.Lookup(tool.lookupName())
	if len(locations) == 0 {
		return found
	}

	found.Found = true
	found.Path = locations[0]
	if len(locations) > 1 {
		found.Shadowed = locations[1:]
	}

	if len(tool.args) == 0 {
		return found
	}

	output, err := deps.Run(ctx, proc.Command{
		Name:    found.Path,
		Args:    tool.args,
		Timeout: timeout,
	})
	switch {
	case err != nil:
		// The tool is installed and would not answer. That is worth knowing
		// and is not a reason to fail the command.
		found.Detail = err.Error()
		return found
	case output.ExitCode != 0:
		found.Detail = "exited " + strconv.Itoa(output.ExitCode)
	}

	found.Version = version(output.Combined())
	if found.Version == "" && found.Detail == "" {
		found.Detail = "no version could be read from its output"
	}
	return found
}
