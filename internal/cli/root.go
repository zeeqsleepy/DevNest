package cli

// NewRoot builds the command tree.
//
// Feature commands are registered here as they are implemented. Nothing else
// changes when one is added, which is the property the layering exists to
// protect.
func NewRoot() *Command {
	root := &Command{
		Name:        "devnest",
		Summary:     tagline,
		Description: tagline,
		Usage:       "devnest <command> [arguments] [flags]",
		Commands: []*Command{
			newCleanCommand(),
			newCompletionCommand(),
			newDecodeCommand(),
			newDoctorCommand(),
			newEncodeCommand(),
			newEnvCommand(),
			newFileCommand(),
			newGitCommand(),
			newHelpCommand(),
			newJSONCommand(),
			newLogCommand(),
			newNetworkCommand(),
			newPortCommand(),
			newScanCommand(),
			newSecretCommand(),
			newSecurityCommand(),
			newVersionCommand(),
			newYAMLCommand(),
		},
	}
	root.link()
	return root
}
