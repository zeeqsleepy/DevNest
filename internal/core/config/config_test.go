package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	base "github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
)

// fakeFilesystem is a disk in a map. The configuration file is read by
// internal/config through the real filesystem, so the tests that need loading
// use a temporary directory; this fake covers the writing side, where what
// matters is exactly what would have been written.
type fakeFilesystem struct {
	files      map[string][]byte
	writeErr   error
	lastWrite  string
	writeCount int
}

func newFake() *fakeFilesystem {
	return &fakeFilesystem{files: map[string][]byte{}}
}

func (f *fakeFilesystem) Exists(path string) (bool, error) {
	_, ok := f.files[path]
	return ok, nil
}

func (f *fakeFilesystem) ReadFile(path string) ([]byte, error) {
	contents, ok := f.files[path]
	if !ok {
		return nil, errors.New(errors.CodeNotFound, "no such file: %s", path)
	}
	return contents, nil
}

func (f *fakeFilesystem) WriteAtomic(path string, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files[path] = data
	f.lastWrite = string(data)
	f.writeCount++
	return nil
}

// onDisk writes a real file, for the paths that go through the loader.
func onDisk(t *testing.T, contents string) (*fakeFilesystem, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	fake := newFake()
	if contents != "" {
		fake.files[path] = []byte(contents)
		writeReal(t, path, contents)
	}
	return fake, path
}

// request points at a file the test owns. Explicit stays false, because a file
// that is not there yet is the ordinary state of a machine nobody has
// configured, and "config set" has to work on one.
func request(path string) Request {
	return Request{
		Path:      path,
		LookupEnv: func(string) (string, bool) { return "", false },
	}
}

// "Why is it behaving like that" is the question this command exists for, and
// the layer a value came from is most of the answer.
func TestShowReportsWhereEachValueCameFrom(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	req := request(path)
	req.LookupEnv = func(name string) (string, bool) {
		if name == "DEVNEST_NETWORK_TIMEOUT_MS" {
			return "5000", true
		}
		return "", false
	}

	result, err := Show(fake, req)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	origins := map[string]string{}
	for _, value := range result.Values {
		origins[value.Key] = value.Origin
	}
	if origins["general.output"] != base.OriginFile {
		t.Errorf("general.output came from %q, want %q", origins["general.output"], base.OriginFile)
	}
	if origins["network.timeout_ms"] != base.OriginEnvironment {
		t.Errorf("network.timeout_ms came from %q, want %q",
			origins["network.timeout_ms"], base.OriginEnvironment)
	}
	if origins["general.color"] != base.OriginDefault {
		t.Errorf("general.color came from %q, want %q", origins["general.color"], base.OriginDefault)
	}
	if result.FromFile != 1 || result.FromEnvironment != 1 {
		t.Errorf("counted %d from the file and %d from the environment",
			result.FromFile, result.FromEnvironment)
	}
}

func TestGetNamesAKeyItDoesNotHave(t *testing.T) {
	fake, path := onDisk(t, "")
	_ = fake

	_, err := Get(request(path), "general.outpu")
	if err == nil {
		t.Fatal("an unknown key was accepted")
	}
	if hint := errors.Classify(err).Hint; !strings.Contains(hint, "general.output") {
		t.Errorf("hint = %q, want it to suggest general.output", hint)
	}
}

// A rejected value must leave the previous file alone: the command that fixes
// a broken configuration cannot be the command that breaks one.
func TestSetRefusesAValueTheSchemaRejects(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	if _, err := Set(fake, request(path), "general.output", "pdf"); err == nil {
		t.Fatal("an invalid value was written")
	}
	if fake.writeCount != 0 {
		t.Errorf("the file was written %d time(s) despite the refusal", fake.writeCount)
	}

	if _, err := Set(fake, request(path), "security.password_length", "4"); err == nil {
		t.Error("a value outside the accepted range was written")
	}
}

func TestSetKeepsTheRestOfTheFile(t *testing.T) {
	fake, path := onDisk(t, "# mine\n[general]\noutput = \"json\"\nverbosity = \"debug\"\n")

	result, err := Set(fake, request(path), "general.output", "table")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !result.Changed || result.Previous != "json" || result.Value != "table" {
		t.Errorf("result = %+v", result)
	}
	for _, want := range []string{"# mine", `output = "table"`, `verbosity = "debug"`} {
		if !strings.Contains(fake.lastWrite, want) {
			t.Errorf("%q is missing from the written file:\n%s", want, fake.lastWrite)
		}
	}
}

func TestSetCreatesTheFileWhenThereIsNone(t *testing.T) {
	fake := newFake()
	path := filepath.Join(t.TempDir(), "config.toml")

	result, err := Set(fake, request(path), "general.output", "json")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !result.Created || !strings.Contains(fake.lastWrite, `output = "json"`) {
		t.Errorf("result = %+v, file:\n%s", result, fake.lastWrite)
	}
}

func TestUnsetRestoresTheDefault(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	result, err := Unset(fake, request(path), "general.output")
	if err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if !result.Changed || result.Value != "table" {
		t.Errorf("result = %+v, want the default back", result)
	}
	if strings.Contains(fake.lastWrite, "output =") {
		t.Errorf("the key is still in the file:\n%s", fake.lastWrite)
	}
}

// Removing a key that was never there is a success with nothing written, not
// an error: the state the user asked for is the state they already had.
func TestUnsetOfAKeyThatIsNotThereWritesNothing(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	result, err := Unset(fake, request(path), "general.color")
	if err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if result.Changed || fake.writeCount != 0 {
		t.Errorf("result = %+v, writes = %d", result, fake.writeCount)
	}

	if _, err := Unset(fake, request(path), "general.nonsense"); err == nil {
		t.Error("an unknown key was accepted")
	}
}

// The file holds decisions somebody made by hand. Overwriting it because a
// command was typed twice would be the worst thing this module could do.
func TestInitRefusesToOverwrite(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	if _, err := Init(fake, request(path)); err == nil {
		t.Fatal("an existing file was overwritten")
	}
	if fake.writeCount != 0 {
		t.Errorf("the file was written %d time(s)", fake.writeCount)
	}
}

func TestInitWritesEveryKey(t *testing.T) {
	fake := newFake()
	path := filepath.Join(t.TempDir(), "config.toml")

	result, err := Init(fake, request(path))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !result.Created {
		t.Errorf("result = %+v", result)
	}
	for _, key := range base.Keys() {
		_, name, _ := strings.Cut(key, ".")
		if !strings.Contains(fake.lastWrite, name+" = ") {
			t.Errorf("%s is missing from the written file", key)
		}
	}
}

func TestValidateReportsAGoodFileAndABadOne(t *testing.T) {
	fake, path := onDisk(t, "[general]\noutput = \"json\"\n")

	result, err := Validate(fake, request(path))
	if err != nil || !result.Valid || result.Keys != 1 {
		t.Fatalf("Validate = %+v, %v", result, err)
	}

	broken, brokenPath := onDisk(t, "[general]\noutput = \"pdf\"\n")
	if _, err := Validate(broken, request(brokenPath)); err == nil {
		t.Error("a file holding an unusable value was reported as valid")
	}
}

// A file with no keys at all is a valid file, and so is no file.
func TestValidateAcceptsAnAbsentFile(t *testing.T) {
	fake := newFake()
	path := filepath.Join(t.TempDir(), "config.toml")

	result, err := Validate(fake, request(path))
	if err != nil || !result.Valid || result.Exists {
		t.Errorf("Validate = %+v, %v", result, err)
	}
}

// writeReal puts the file on the real disk, because internal/config reads it
// there. The fake above still records what a command would have written.
func writeReal(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
