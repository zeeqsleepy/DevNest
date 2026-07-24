// Package version exposes metadata about the running build.
//
// The values are injected at link time by the release build; a plain
// "go build" produces a binary that reports the fallback values below.
package version

import "runtime"

// Injected with -ldflags -X. See the Makefile.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// Info describes the running build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns metadata for the running build.
func Get() Info {
	return Info{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// Short returns the version alone, for use in the output envelope.
func Short() string { return version }

// String returns a one-line summary such as "devnest 1.2.3 (a1b2c3d)".
func (i Info) String() string {
	return "devnest " + i.Version + " (" + i.Commit + ")"
}
