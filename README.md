# bootstrap_dev_env

A Go-based bootstrap tool that installs a development environment across
macOS, Debian/Ubuntu, RHEL/Fedora, and Arch Linux on `x86_64` and `aarch64`.
It installs system packages, optional Flatpak GUI apps, and a set of custom
third-party tools (Go, Neovim, Zig, NVM, pyenv, oh-my-zsh, Firecracker on
Linux).

## Install

Download the pre-built binary for your platform and make it executable.

Using **curl**:

```shell
curl -Lo bootstrap_environment "https://github.com/JMR-dev/bootstrap_dev_env/releases/latest/download/bootstrap_environment-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/; s/arm64/arm64/')"
chmod +x bootstrap_environment
```

Using **wget**:

```shell
wget -O bootstrap_environment "https://github.com/JMR-dev/bootstrap_dev_env/releases/latest/download/bootstrap_environment-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/; s/arm64/arm64/')"
chmod +x bootstrap_environment
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

Package lists live in `packages.go`. Edit and rebuild.

## Build from Source

If you prefer to build the tool yourself, or if you are making custom modifications:

1. Clone the repository:
   ```shell
   git clone https://github.com/JMR-dev/bootstrap_dev_env.git
   cd bootstrap_dev_env
   ```

2. Build for the current host platform:
   ```shell
   make build           # builds ./bootstrap_environment for the host
   ```

To cross-compile binaries for all supported targets at once:
```shell
make build-all       # writes dist/bootstrap_environment-{linux,darwin}-{amd64,arm64}
```
