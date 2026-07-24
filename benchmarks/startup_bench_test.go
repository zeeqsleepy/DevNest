package benchmarks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Startup is the one measurement that has to run the built binary rather than
// call a function: what the target in docs/performance.md means is the time
// between pressing return and reading the answer, and most of that is the
// process starting, resolving configuration, and building the command tree.
//
// It is also the metric most likely to degrade unnoticed, because nothing in a
// unit test gets slower when the binary gains an init function.
func BenchmarkStartupVersion(b *testing.B) {
	binary := benchBinary(b)
	config := filepath.Join(b.TempDir(), "config.toml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		b.Fatalf("write empty configuration: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		command := exec.Command(binary, "version", "--config", config)
		command.Stdout = nil
		command.Stderr = nil
		if err := command.Run(); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// benchBinary locates the built binary, skipping rather than failing when it is
// not there: "go test ./benchmarks" without a build should not look broken.
func benchBinary(b *testing.B) string {
	b.Helper()

	if path := os.Getenv("DEVNEST_BINARY"); path != "" {
		return path
	}

	name := "devnest"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join("..", "dist", name)

	if _, err := os.Stat(path); err != nil {
		b.Skipf("binary not found at %s; run \"make build\" first", path)
	}
	return path
}
