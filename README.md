# bootstrap_dev_env

A Go-based bootstrap tool that provisions a development environment across
macOS, Debian/Ubuntu, RHEL/Fedora, and Arch Linux on `x86_64` and `aarch64`.

It installs:

- **System packages** via `dnf`, `apt-get`, `pacman`, or `brew` (macOS)
- **Flatpak GUI apps** from Flathub (Linux only, opt-in with `--gui`)
- **Custom third-party tools** fetched and verified directly:
  Go, Neovim, Zig, Firecracker (Linux), NVM, pyenv, pip, oh-my-zsh,
  agy, claude, codex, copilot, playwright, and the `gh-repo-bootstrap` extension

## Supported platforms

| OS | Architecture | Package manager |
|---|---|---|
| Linux (Debian/Ubuntu) | x86_64 | apt-get |
| Linux (RHEL/Fedora) | x86_64 | dnf |
| Linux (Arch) | x86_64 | pacman |
| macOS | arm64 | brew |

> **Note:** Linux arm64 (`aarch64`) binaries are cross-compiled but not yet tested in CI.

## CI

| Workflow | Trigger |
|---|---|
| **Unit Tests** — `go test -v ./...` + `go vet ./...` | Pull requests to `main` / `dev` |
| **Integration Tests** — full run on Debian, Arch, Fedora, Ubuntu (Dagger) and macOS (native) | Push + pull requests to `main` / `dev` |

## Install

Build from source:

```shell
git clone https://github.com/JMR-dev/bootstrap_dev_env.git
cd bootstrap_dev_env
make build           # builds ./bootstrap_environment for the host
```

Cross-compile all supported targets at once:

```shell
make build-all       # writes dist/bootstrap_environment-{linux,darwin}-{amd64,arm64,...}
```

Or download a pre-built binary from the [Releases](https://github.com/JMR-dev/bootstrap_dev_env/releases) page.

## Usage

```shell
# Linux (do NOT use sudo on macOS — Homebrew refuses to run as root)
sudo ./bootstrap_environment [flags]

# macOS
./bootstrap_environment [flags]
```

### Flags

| Flag | Description |
|---|---|
| `--only system\|flatpak\|custom` | Restrict to a single section |
| `--gui` | Include GUI applications and the Flatpak section (default: headless — both skipped) |
| `--no-vm` | macOS only: skip provisioning the Fedora-on-QEMU VM that backs the `firecracker()` zsh wrapper |
| `--no-ai` | Skip AI/LLM CLI tools (agy, claude, codex, copilot) |

### Example

```shell
# Full headless install (typical CI / server)
sudo ./bootstrap_environment

# Desktop workstation — include GUI apps and Flatpaks
sudo ./bootstrap_environment --gui

# Install only system packages, skipping AI tools
sudo ./bootstrap_environment --only system --no-ai

# macOS, no VM provisioning
./bootstrap_environment --gui --no-vm
```

## Configuration

Package lists live in `packages.go`. Edit the `SystemPackages`,
`FlatpakPackages`, or `customPackages()` slices and rebuild.

Platform-specific name mappings (e.g. `docker-ce-rootless-extras` →
skipped on Arch, `ffmpeg-free` → `ffmpeg` on apt-get) live in the
`packageOverrides` map in `pkgmgr.go`. Third-party repository setup
(Docker, GitHub CLI, Temurin, lazygit COPR, etc.) lives in `repos.go`.

## Development

```shell
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
make build   # build host binary
```

