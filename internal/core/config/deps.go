package config

// Filesystem is what this module needs of the disk: read a file, notice
// whether it is there, and replace it in one step.
//
// The write is atomic in the implementation below the module, because a
// configuration file truncated by an interrupted write is a tool that will not
// start until somebody finds and deletes it.
type Filesystem interface {
	Exists(path string) (bool, error)
	ReadFile(path string) ([]byte, error)
	WriteAtomic(path string, data []byte) error
}
