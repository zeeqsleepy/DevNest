# Installation

Status: current as of 0.1.0. The archives, the `.deb`, the `.rpm`, and building from source all
work today; winget and Homebrew are generated with each release but not submitted yet
Last revised: 2026-07-25

DevNest is a single executable with no runtime dependency. Every method below results in the same
binary; pick whichever fits how you manage tools.

## Windows

### winget

*Not submitted yet.* The manifest is generated with every release and attached to it, and it is
submitted to the winget repository once there is a release worth installing.

```powershell
winget install zeeqsleepy.DevNest
```

### Scoop

*Not submitted yet*, for the same reason. Until then, the manual method below is the Windows
install.

### Manual

1. Download `devnest-windows-amd64.zip` from the releases page. On an ARM machine, take
   `devnest-windows-arm64.zip`.
2. Extract `devnest.exe` to a directory you control: `%LOCALAPPDATA%\Programs\devnest` is a
   reasonable choice and needs no elevation.
3. Add that directory to PATH:

```powershell
[Environment]::SetEnvironmentVariable(
    "Path",
    [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:LOCALAPPDATA\Programs\devnest",
    "User"
)
```

4. Open a new terminal and run `devnest version`.

A locked-down corporate machine that blocks package managers is exactly the case the manual method
is for. The binary needs no installer, no elevation, and no runtime.

## macOS

### Homebrew

*Not published yet.* DevNest cannot go into Homebrew core, which requires an OSI-approved licence
that the Commons Clause rules out, so it will live in a self-hosted tap:

```bash
brew tap zeeqsleepy/devnest
brew install --cask devnest
```

### Manual

```bash
curl -LO https://github.com/zeeqsleepy/DevNest/releases/latest/download/devnest-darwin-arm64.tar.gz
tar -xzf devnest-darwin-arm64.tar.gz
sudo mv devnest /usr/local/bin/
devnest version
```

Use `devnest-darwin-amd64.tar.gz` on an Intel Mac.

The binary is not notarised by Apple, so Gatekeeper will block it on first run. To allow it:

```bash
xattr -d com.apple.quarantine /usr/local/bin/devnest
```

Or open System Settings → Privacy & Security and allow it there after the first blocked attempt.

## Linux

### Debian and Ubuntu

```bash
curl -LO https://github.com/zeeqsleepy/DevNest/releases/latest/download/devnest_linux_amd64.deb
sudo dpkg -i devnest_linux_amd64.deb
```

### Fedora and RHEL

```bash
curl -LO https://github.com/zeeqsleepy/DevNest/releases/latest/download/devnest_linux_amd64.rpm
sudo rpm -i devnest_linux_amd64.rpm
```

### Manual

```bash
curl -LO https://github.com/zeeqsleepy/DevNest/releases/latest/download/devnest-linux-amd64.tar.gz
tar -xzf devnest-linux-amd64.tar.gz
sudo mv devnest /usr/local/bin/
devnest version
```

Use `devnest-linux-arm64.tar.gz` on ARM.

For a user-local install with no `sudo`, move it to `~/.local/bin` and make sure that is on PATH.

## From source

Requires Go 1.25 or newer.

```bash
go install github.com/devnest/devnest/cmd/devnest@latest
```

The binary lands in `$(go env GOPATH)/bin`, which needs to be on PATH.

For a specific version, replace `@latest` with a tag: `@v1.2.3`.

To build from a clone:

```bash
git clone https://github.com/zeeqsleepy/DevNest.git
cd DevNest
make build
```

Output goes to `dist/`.

## Verifying the download

Every release publishes `checksums.txt` with SHA-256 for every artifact. Worth checking on a
manual download.

Windows:

```powershell
Get-FileHash devnest-windows-amd64.zip -Algorithm SHA256
```

macOS and Linux:

```bash
sha256sum devnest-linux-amd64.tar.gz
```

Compare against the corresponding line in `checksums.txt`.

## Shell completion

Completion scripts are printed to stdout for you to place wherever your shell expects them.

**PowerShell**: add to your profile (`$PROFILE`):

```powershell
devnest completion powershell | Out-String | Invoke-Expression
```

To make it permanent without regenerating on every start:

```powershell
devnest completion powershell > "$(Split-Path $PROFILE)\devnest-completion.ps1"
# then add to $PROFILE:
. "$(Split-Path $PROFILE)\devnest-completion.ps1"
```

**bash**:

```bash
devnest completion bash | sudo tee /etc/bash_completion.d/devnest > /dev/null
```

**zsh**:

```bash
devnest completion zsh > "${fpath[1]}/_devnest"
```

**fish**:

```bash
devnest completion fish > ~/.config/fish/completions/devnest.fish
```

Restart the shell afterwards.

Each script is generated from the command tree inside the binary that printed it, so it completes
exactly the commands and flags that version has. Regenerate it after an upgrade.

## Verifying the installation

```
devnest version
devnest doctor
```

`doctor` checks that the configuration parses, the config directory is writable, embedded rule
sets load, and terminal capabilities are detected correctly. Its output is what to attach to a
bug report.

## Updating

Whatever installed it, updates it:

```powershell
winget upgrade zeeqsleepy.DevNest
scoop update devnest
```

```bash
brew upgrade devnest
go install github.com/devnest/devnest/cmd/devnest@latest
```

For a manual install, download the new archive and replace the binary.

**DevNest never checks for updates on its own.** No background check, no startup ping, no
notification. The binary makes no network connection except when you run `devnest http`. Checking
for a new version is your package manager's job, or the releases page.

## Uninstalling

```powershell
winget uninstall zeeqsleepy.DevNest
scoop uninstall devnest
```

```bash
brew uninstall devnest
sudo dpkg -r devnest
sudo rpm -e devnest
sudo rm /usr/local/bin/devnest
```

Configuration is left behind by every method. To remove it:

| Platform | Path |
|---|---|
| Windows | `%APPDATA%\devnest` |
| Linux | `~/.config/devnest` |
| macOS | `~/Library/Application Support/devnest` |

Nothing else is written anywhere. No registry keys, no service registration, no cache directory,
no data outside the config directory you just deleted.
