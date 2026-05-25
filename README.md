# bootstrap_dev_env

A Go-based bootstrap tool that installs a development environment across
macOS, Debian/Ubuntu, RHEL/Fedora, and Arch Linux on `x86_64` and `aarch64`.
It installs system packages, optional Flatpak GUI apps, and a set of custom
third-party tools (Go, Neovim, Zig, NVM, pyenv, oh-my-zsh, Firecracker on
Linux, agy, claude, codex, and copilot).

## Install

Download the native binary for your platform from a release, or build from
source:

```shell
git clone https://github.com/JMR-dev/bootstrap_dev_env.git
cd bootstrap_dev_env
make build           # builds ./bootstrap_environment for the host
```

To produce native binaries for all four supported targets at once:

```shell
make build-all       # writes dist/bootstrap_environment-{linux,darwin}-{amd64,arm64}
```

## Usage

```shell
# Linux (do NOT use sudo on macOS — Homebrew refuses to run as root)
sudo ./bootstrap_environment [--only system|flatpak|custom] [--gui]

# macOS
./bootstrap_environment [--only system|custom] [--gui] [--no-vm]
```

Flags:

- `--only` — restrict to one section (`system`, `flatpak`, or `custom`).
- `--gui`  — include GUI applications and the Flatpak section. Default is
  headless: both are skipped.
- `--no-vm` — macOS only: skip provisioning the Fedora-on-QEMU/VirtualBox VM
  that backs the `firecracker()` zsh wrapper.
- `--no-ai` — skip installation of AI/LLM CLI tools (agy, claude, codex, copilot).

Package lists live in `packages.go`. Edit and rebuild.
