# DevNest

A single-binary command line toolkit for the routine work that surrounds writing code.

Which process holds port 5173? How big has `node_modules` grown across every project on this
machine? Did anyone commit an API key last week? What is the SHA-256 of this artifact? What is
actually inside this JWT?

Those problems are solved somewhere, but each on a different command, per operating system, with a
personal pile of extra utilities and a browser tab for the rest. DevNest collects them into one
executable with one consistent interface that behaves identically on Windows, Linux, and macOS.

> **Status: 1.0.0.** The compatibility promise has begun: command names, flag names, JSON field
> names, and exit codes are frozen for the major version. See [docs/roadmap.md](docs/roadmap.md).

## Install

**macOS (Homebrew)**
```bash
brew tap zeeqsleepy/devnest
brew install --cask devnest
```

**Linux (Debian/Ubuntu)**, or the `.rpm` for Fedora/RHEL:
```bash
curl -LO https://github.com/zeeqsleepy/DevNest/releases/latest/download/devnest_linux_amd64.deb
sudo dpkg -i devnest_linux_amd64.deb
```

**Without a package manager**: Windows, or a locked-down machine with no installer. Download the
archive for your platform from the [releases page](https://github.com/zeeqsleepy/DevNest/releases),
extract the binary, and put it on PATH. Done. No runtime, no elevation, no installer.

**From source** (Go 1.25+):
```bash
go install github.com/devnest/devnest/cmd/devnest@latest
```

Verify with `devnest version` and `devnest doctor`. More detail, checksum verification, shell
completion, and uninstalling are in [docs/installation.md](docs/installation.md).

## What it does

Every command reads plain JSON with `--output json` and returns a meaningful exit code, so the
same tool works on a terminal and in a CI script.

```
devnest file organize ~/Downloads    group files into Images/, Documents/, ...
devnest file duplicate ~/Pictures    find identical files by content
devnest file hash installer.exe      SHA-256, SHA-512, MD5
devnest file size C:\projects        where the disk space went

devnest network http example.com     one request, status, timing, headers
devnest network ping example.com     is the host reachable (TCP probe)
devnest network scan example.com     which ports are open, and why
devnest network ssl example.com      issuer, expiry, days left, trust

devnest security password            a strong password, system-random
devnest security checksum f.zip <hash>  verify a download
devnest security password-check --stdin  score a password, say what's wrong
devnest decode jwt --stdin           a token's header, claims, expiry

devnest log http access.log          requests, methods, statuses, top endpoints
devnest log errors app.log           failures grouped and counted
devnest log search app.log timeout   every matching line, with line numbers

devnest env                          os, shell, toolchains, PATH health
devnest env which python --versions  every python on PATH and what it reports

devnest scan                         what a project is made of
devnest scan compare baseline.json   how it grew since an earlier scan
devnest clean . --apply              remove build and dependency directories

devnest git                          branch, remotes, counts, age
devnest git hotspot                  the files a repository changes most often
devnest secret scan --fail-on high   credentials, as a CI gate
devnest secret history --all         credentials ever committed

devnest json format api.json         one indentation width, key order kept
devnest json query api.json users[0] select a subtree
devnest yaml to-json manifest.yaml   convert

devnest port list / check / free     what is listening, and which process holds it
devnest config get key               why is DevNest behaving like that
devnest init --template go-cli api   scaffold a new project
```

The full command surface and every flag are in [docs/commands.md](docs/commands.md) and
[docs/cli-reference.md](docs/cli-reference.md).

## How it behaves

- **One binary, no runtime.** A static Go executable. Download, put on PATH, done.
- **Identical on every platform.** Where the operating systems differ, the difference is absorbed
  inside DevNest; Windows is the primary development platform, not a port done afterwards.
- **Every command speaks JSON.** `--output json` on anything. The table view is a rendering of the
  same data, and the JSON never contains less.
- **Nothing leaves your machine unasked.** Only `devnest network` opens a socket, and only to the
  address you named. No telemetry, no analytics, no update check, not even opt-in.
- **Safe by default.** Nothing destructive happens without an explicit flag. `devnest clean`
  deletes only known build/dependency directories, re-checks each one immediately before removing
  it, and does nothing at all without `--apply`. A password given to a security command is never
  logged, stored, or printed.

## Documentation

- **Maybe start here**: [quick start](docs/quick-start.md) · [commands](docs/commands.md) ·
  [configuration](docs/configuration.md) · [FAQ](docs/faq.md) ·
  [troubleshooting](docs/troubleshooting.md)
- **For maintainers**: [architecture](docs/architecture.md) ·
  [modules](docs/modules.md) · [roadmap](docs/roadmap.md) ·
  [development guide](docs/development-guide.md) · [CONTRIBUTING.md](CONTRIBUTING.md)

## License

[MIT](LICENSE). Use it, change it, redistribute it, sell it, put it in a paid product. Keep the
copyright notice and there are no other conditions.
