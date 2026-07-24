package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/port"
)

func listener(number, pid int, process, scope string) port.Listener {
	return port.Listener{
		Protocol: "tcp",
		Address:  "0.0.0.0",
		Port:     number,
		Scope:    scope,
		PID:      pid,
		Process:  process,
		Known:    process != "",
	}
}

func TestPortListTextSaysHowReachableEachSocketIs(t *testing.T) {
	result := port.ListResult{
		Listeners: []port.Listener{
			listener(3000, 8124, "node", port.ScopeAllInterfaces),
			listener(5432, 900, "postgres", port.ScopeLoopback),
		},
		Count: 2,
	}

	got := render(t, portListText(result))
	for _, want := range []string{"3000", "node", "all interfaces", "this machine only"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// A socket whose owner the system would not name is still shown, and it is
// shown as unknown rather than as a blank cell that reads like a bug.
func TestPortListTextNamesTheGap(t *testing.T) {
	result := port.ListResult{
		Listeners: []port.Listener{listener(3000, 0, "", port.ScopeLoopback)},
		Count:     1,
	}

	got := render(t, portListText(result))
	if !strings.Contains(got, "unknown") {
		t.Errorf("output = %q, want the unnamed owner marked", got)
	}
	if !strings.Contains(got, "-") {
		t.Errorf("output = %q, want the missing pid shown as a dash", got)
	}
}

// Hiding the system ports is fine; hiding the fact that they were hidden is
// not, because a short list would read as a quiet machine.
func TestPortListTextReportsWhatItHid(t *testing.T) {
	result := port.ListResult{
		Listeners:    []port.Listener{listener(3000, 1, "node", port.ScopeLoopback)},
		Count:        1,
		SystemHidden: 12,
	}

	got := render(t, portListText(result))
	if !strings.Contains(got, "12") || !strings.Contains(got, "--all") {
		t.Errorf("output = %q, want the hidden count and the flag that shows them", got)
	}
}

func TestPortListTextHandlesAQuietMachine(t *testing.T) {
	got := render(t, portListText(port.ListResult{}))

	if !strings.Contains(got, "Nothing is listening") {
		t.Errorf("output = %q, want a sentence rather than an empty table", got)
	}
}

func TestPortCheckTextAnswersInOneLineWhenFree(t *testing.T) {
	free := render(t, portCheckText(port.CheckResult{Port: 3000}))
	if free != "Port 3000 is free.\n" {
		t.Errorf("output = %q, want a single line", free)
	}

	taken := render(t, portCheckText(port.CheckResult{
		Port:      3000,
		InUse:     true,
		Listeners: []port.Listener{listener(3000, 8124, "node", port.ScopeAllInterfaces)},
	}))
	for _, want := range []string{"in use", "node", "8124"} {
		if !strings.Contains(taken, want) {
			t.Errorf("output = %q, want it to contain %q", taken, want)
		}
	}
}

// The question names the process. "Terminate the process on port 3000?" is not
// something anybody can answer; "Ask node (pid 8124) to exit?" is.
func TestFreeQuestionNamesTheProcess(t *testing.T) {
	holder := port.CheckResult{
		Port:      3000,
		InUse:     true,
		Listeners: []port.Listener{listener(3000, 8124, "node", port.ScopeAllInterfaces)},
	}

	polite := freeQuestion(holder, false)
	if !strings.Contains(polite, "node") || !strings.Contains(polite, "8124") {
		t.Errorf("question = %q, want the process named", polite)
	}
	if !strings.Contains(polite, "exit") {
		t.Errorf("question = %q, want it to describe a request rather than a kill", polite)
	}

	forced := freeQuestion(holder, true)
	if !strings.Contains(forced, "Kill") || !strings.Contains(forced, "unsaved") {
		t.Errorf("question = %q, want it to say what --force costs", forced)
	}
}

func TestFreeQuestionStaysHonestWhenTheProcessIsUnknown(t *testing.T) {
	holder := port.CheckResult{
		Port:      3000,
		InUse:     true,
		Listeners: []port.Listener{listener(3000, 0, "", port.ScopeLoopback)},
	}

	question := freeQuestion(holder, false)
	if strings.Contains(question, "pid 0") {
		t.Errorf("question = %q, want no invented process detail", question)
	}
}

func TestPortFreeTextDistinguishesAskedFromKilled(t *testing.T) {
	asked := render(t, portFreeText(port.FreeResult{
		Port:     3000,
		Target:   listener(3000, 8124, "node", port.ScopeLoopback),
		Graceful: true,
		Freed:    true,
	}))
	if !strings.Contains(asked, "exited on request") || !strings.Contains(asked, "free") {
		t.Errorf("output = %q, want a graceful exit reported as one", asked)
	}

	killed := render(t, portFreeText(port.FreeResult{
		Port:   3000,
		Target: listener(3000, 8124, "node", port.ScopeLoopback),
		Freed:  false,
	}))
	if !strings.Contains(killed, "was killed") || !strings.Contains(killed, "still held") {
		t.Errorf("output = %q, want the kill and the lingering socket reported", killed)
	}
}

func TestPortArgumentRejectsWhatIsNotAPort(t *testing.T) {
	for _, args := range [][]string{{}, {"http"}, {"0"}, {"70000"}, {"80", "443"}} {
		if _, err := portArgument(args); err == nil {
			t.Errorf("portArgument(%v) was accepted", args)
		}
	}

	number, err := portArgument([]string{"3000"})
	if err != nil || number != 3000 {
		t.Errorf("portArgument([3000]) = %d, %v", number, err)
	}
}
