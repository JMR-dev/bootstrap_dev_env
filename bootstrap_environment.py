#!/usr/bin/env python3
"""
Bootstrap packages declared in formatted_packages.py.

Sections handled:
  System Packages  — installed via dnf or apt-get
  Flatpak Packages — installed via flatpak from Flathub (skipped with --no-gui)
  Custom Packages  — downloaded, verified, extracted

Usage:
  sudo python3 bootstrap_environment.py [--only system|flatpak|custom] [--no-gui]
"""

import argparse
import datetime
import getpass
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

import formatted_packages

SCRIPT_DIR = Path(__file__).parent
RUN_LOG = SCRIPT_DIR / "bootstrap_run.log"

# ── issue log ─────────────────────────────────────────────────────────────────

_issues: list[str] = []
_issues_lock = threading.Lock()

def _log_issue(level: str, msg: str) -> None:
    with _issues_lock:
        print(f"  [{level}] {msg}")
        _issues.append(f"[{level}] {msg}")

def warn(msg: str) -> None:
    _log_issue("WARN", msg)

def err(msg: str) -> None:
    _log_issue("ERROR", msg)

def write_run_log() -> None:
    if not _issues:
        print("\nNo issues — log file not written.")
        return
    timestamp = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    lines = [f"# Bootstrap run — {timestamp}", ""] + _issues
    RUN_LOG.write_text("\n".join(lines) + "\n")
    print(f"\n{len(_issues)} issue(s) logged to: {RUN_LOG}")

# ── subprocess helpers ────────────────────────────────────────────────────────

def run(
    cmd: list,
    *,
    as_sudo: bool = False,
    check: bool = True,
    input: Optional[bytes] = None,
    capture_output: bool = False,
    cwd: Optional[str] = None,
) -> subprocess.CompletedProcess:
    if as_sudo and os.geteuid() != 0:
        cmd = ["sudo"] + cmd
    print(f"  $ {' '.join(str(c) for c in cmd)}")
    return subprocess.run(
        cmd, check=check, input=input,
        capture_output=capture_output, cwd=cwd,
    )

def shell(
    cmd: str,
    *,
    check: bool = True,
    capture_output: bool = False,
    text: bool = False,
) -> subprocess.CompletedProcess:
    print(f"  $ {cmd}")
    return subprocess.run(
        cmd, shell=True, check=check,
        capture_output=capture_output, text=text,
    )

def has_cmd(name: str) -> bool:
    return shutil.which(name) is not None

# ── architecture detection ────────────────────────────────────────────────────

# Tokens commonly seen in download URLs / asset names per architecture.
_ARCH_TOKENS: dict[str, tuple[str, ...]] = {
    "x86_64":  ("x86_64", "amd64", "x64"),
    "aarch64": ("aarch64", "arm64"),
}

def detect_arch() -> str:
    m = platform.machine().lower()
    if m in ("x86_64", "amd64"):
        return "x86_64"
    if m in ("aarch64", "arm64"):
        return "aarch64"
    sys.exit(f"Unsupported architecture: {platform.machine()} (supports x86_64, aarch64)")

ARCH = detect_arch()

# Per-arch substitutions used by repo / download URL construction.
_ARCH_GO       = {"x86_64": "amd64",  "aarch64": "arm64"}
_ARCH_MINIKUBE = {"x86_64": "amd64",  "aarch64": "arm64"}
_ARCH_DEB      = {"x86_64": "amd64",  "aarch64": "arm64"}
_ARCH_NVIM     = {"x86_64": "x86_64", "aarch64": "arm64"}

def _url_format(template: str, version: Optional[str]) -> str:
    """Interpolate {version}, {arch}, {arch_go} into a URL template."""
    return template.format(version=version or "", arch=ARCH, arch_go=_ARCH_GO[ARCH])

def _arch_matches(name: str, arch: str = ARCH) -> bool:
    n = name.lower()
    return any(tok in n for tok in _ARCH_TOKENS[arch])

def _other_arch() -> str:
    return "aarch64" if ARCH == "x86_64" else "x86_64"

def _has_other_arch_token(name: str) -> bool:
    n = name.lower()
    return any(tok in n for tok in _ARCH_TOKENS[_other_arch()])

# ── sudo prereq ───────────────────────────────────────────────────────────────

def check_sudo() -> None:
    if os.geteuid() == 0:
        return
    if not has_cmd("sudo"):
        sys.exit("sudo is required but not installed.")
    print("Validating sudo access ...")
    result = subprocess.run(["sudo", "-v"], check=False)
    if result.returncode != 0:
        sys.exit("sudo authentication failed.")

# ── package manager detection ─────────────────────────────────────────────────

def detect_pkg_mgr() -> str:
    for mgr in ("dnf", "apt-get"):
        if has_cmd(mgr):
            return mgr
    sys.exit("No supported package manager found (expected dnf or apt-get).")

PKG_MGR = detect_pkg_mgr()


def _is_rhel_family() -> bool:
    """True for RHEL-derived distros (Fedora, RHEL, CentOS, Rocky, Alma, ...)."""
    try:
        data = Path("/etc/os-release").read_text()
    except OSError:
        return PKG_MGR == "dnf"
    tokens: list[str] = []
    for line in data.splitlines():
        if line.startswith(("ID=", "ID_LIKE=")):
            _, _, val = line.partition("=")
            tokens.extend(val.strip().strip('"').split())
    return any(t in {"rhel", "fedora", "centos", "rocky", "almalinux"} for t in tokens)

IS_RHEL_FAMILY = _is_rhel_family()

# ── network helpers ───────────────────────────────────────────────────────────

def _download(url: str, dest: Path) -> bool:
    """Stream URL → dest. Returns True on success, False on failure (logged)."""
    print(f"  Downloading {Path(url).name} ...")
    try:
        with urllib.request.urlopen(url) as resp, open(dest, "wb") as f:
            shutil.copyfileobj(resp, f, length=1 << 20)
    except (urllib.error.URLError, OSError) as e:
        err(f"Download failed for {url}: {e}")
        return False
    return True

def _fetch_json(url: str) -> Optional[dict]:
    req = urllib.request.Request(url, headers={"Accept": "application/vnd.github+json"})
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        err(f"API request failed for {url}: {e}")
        return None

def _fetch_text(url: str) -> Optional[str]:
    try:
        with urllib.request.urlopen(url) as resp:
            return resp.read().decode().strip()
    except (urllib.error.URLError, OSError) as e:
        err(f"Fetch failed for {url}: {e}")
        return None

# ── installation checks ───────────────────────────────────────────────────────

def is_system_pkg_installed(pkg: str) -> bool:
    if PKG_MGR == "dnf":
        return subprocess.run(["rpm", "-q", pkg], capture_output=True).returncode == 0
    elif PKG_MGR == "apt-get":
        result = subprocess.run(
            ["dpkg-query", "-W", "-f=${Status}", pkg],
            capture_output=True, text=True, check=False,
        )
        return "install ok installed" in result.stdout
    return False

def is_flatpak_installed(app_id: str) -> bool:
    if not has_cmd("flatpak"):
        return False
    return subprocess.run(["flatpak", "info", app_id], capture_output=True).returncode == 0

def is_special_pkg_installed(pkg: str) -> bool:
    if pkg == "obsidian":
        return Path("/usr/local/bin/obsidian").exists()
    if pkg == "minikube":
        return Path("/usr/local/bin/minikube").exists() or has_cmd("minikube")
    if pkg == "bashtop":
        return Path("/usr/local/bin/bashtop").exists() or (Path.home() / "bashtop").exists()
    if pkg == "pipx":
        return has_cmd("pipx")
    if pkg == "poetry":
        return has_cmd("poetry")
    return is_system_pkg_installed(pkg)

# ── package name overrides ────────────────────────────────────────────────────
# Maps distro-specific or unavailable names to their real equivalents.
# None = skip with a warning.

_OVERRIDES: dict[str, dict[str, Optional[list[str]]]] = {
    "dnf": {
        "build-essential": ["gcc", "gcc-c++", "make"],   # Debian meta-package
        "rg":              ["ripgrep"],                   # binary name ≠ package name
        "docker-compose":  None,                          # conflicts with docker-compose-plugin from Docker CE; v2 covers this
        "webcamoid":       None,                          # not in Fedora repos; installed via Flatpak instead
    },
    "apt-get": {
        "ffmpeg-free":     ["ffmpeg"],                    # Fedora-specific name
        "rg":              ["ripgrep"],
    },
}

def resolve_system_pkgs(names: list[str]) -> tuple[list[str], list[str]]:
    """Return (resolved_names, skipped_names) after applying distro overrides."""
    overrides = _OVERRIDES.get(PKG_MGR, {})
    resolved, skipped = [], []
    for pkg in names:
        if pkg in overrides:
            replacement = overrides[pkg]
            if replacement is None:
                skipped.append(pkg)
            else:
                resolved.extend(replacement)
        else:
            resolved.append(pkg)
    return resolved, skipped

# ── repo setup ────────────────────────────────────────────────────────────────

def _repo_file_exists(*paths: str) -> bool:
    return any(Path(p).exists() for p in paths)

def _write_dnf_repo(name: str, display_name: str, baseurl: str, gpgkey: str) -> None:
    content = (
        f"[{name}]\n"
        f"name={display_name}\n"
        f"baseurl={baseurl}\n"
        f"enabled=1\ngpgcheck=1\n"
        f"gpgkey={gpgkey}\n"
    )
    path = f"/etc/yum.repos.d/{name}.repo"
    run(["tee", path], as_sudo=True, input=content.encode(),
        capture_output=True, check=True)

def setup_docker_repo() -> None:
    if PKG_MGR == "dnf":
        if _repo_file_exists("/etc/yum.repos.d/docker-ce.repo"):
            return
        run(["dnf", "config-manager", "addrepo", "--from-repofile",
             "https://download.docker.com/linux/fedora/docker-ce.repo"], as_sudo=True)
    elif PKG_MGR == "apt-get":
        if _repo_file_exists("/etc/apt/sources.list.d/docker.list"):
            return
        run(["apt-get", "install", "-y", "ca-certificates", "curl", "gnupg"], as_sudo=True)
        shell(
            "install -m 0755 -d /etc/apt/keyrings && "
            "curl -fsSL https://download.docker.com/linux/ubuntu/gpg | "
            "sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg && "
            "sudo chmod a+r /etc/apt/keyrings/docker.gpg"
        )
        codename = shell(
            ". /etc/os-release && echo $VERSION_CODENAME",
            capture_output=True, text=True,
        ).stdout.strip()
        deb_arch = _ARCH_DEB[ARCH]
        run(
            ["tee", "/etc/apt/sources.list.d/docker.list"],
            as_sudo=True,
            input=(
                f"deb [arch={deb_arch} signed-by=/etc/apt/keyrings/docker.gpg] "
                f"https://download.docker.com/linux/ubuntu {codename} stable\n"
            ).encode(),
            capture_output=True, check=True,
        )
        run(["apt-get", "update"], as_sudo=True)

def setup_gh_repo() -> None:
    if PKG_MGR == "dnf":
        if _repo_file_exists("/etc/yum.repos.d/gh-cli.repo"):
            return
        run(["dnf", "config-manager", "addrepo", "--from-repofile",
             "https://cli.github.com/packages/rpm/gh-cli.repo"], as_sudo=True)
    elif PKG_MGR == "apt-get":
        if _repo_file_exists("/etc/apt/sources.list.d/github-cli.list"):
            return
        deb_arch = _ARCH_DEB[ARCH]
        shell(
            "curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | "
            "sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && "
            "sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && "
            f"echo 'deb [arch={deb_arch} signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] "
            "https://cli.github.com/packages stable main' | "
            "sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null"
        )
        run(["apt-get", "update"], as_sudo=True)

def setup_chrome_repo() -> None:
    if ARCH != "x86_64":
        warn("Google Chrome has no Linux build for this arch — skipping repo")
        return
    if PKG_MGR == "dnf":
        if _repo_file_exists("/etc/yum.repos.d/google-chrome.repo"):
            return
        _write_dnf_repo(
            "google-chrome", "Google Chrome",
            "https://dl.google.com/linux/chrome/rpm/stable/x86_64",
            "https://dl.google.com/linux/linux_signing_key.pub",
        )
    elif PKG_MGR == "apt-get":
        if _repo_file_exists("/etc/apt/sources.list.d/google-chrome.list"):
            return
        shell(
            "curl -fsSL https://dl.google.com/linux/linux_signing_key.pub | "
            "sudo gpg --dearmor -o /etc/apt/keyrings/google-chrome.gpg && "
            "echo 'deb [arch=amd64 signed-by=/etc/apt/keyrings/google-chrome.gpg] "
            "https://dl.google.com/linux/chrome/deb/ stable main' | "
            "sudo tee /etc/apt/sources.list.d/google-chrome.list > /dev/null && "
            "sudo apt-get update"
        )

def setup_vivaldi_repo() -> None:
    if ARCH != "x86_64":
        warn("Vivaldi repo on this arch is not supported by this script — skipping")
        return
    if PKG_MGR == "dnf":
        if _repo_file_exists("/etc/yum.repos.d/vivaldi.repo"):
            return
        _write_dnf_repo(
            "vivaldi", "Vivaldi",
            "https://repo.vivaldi.com/archive/rpm/x86_64",
            "https://repo.vivaldi.com/archive/linux_signing_key.pub",
        )
    elif PKG_MGR == "apt-get":
        if _repo_file_exists("/etc/apt/sources.list.d/vivaldi.list"):
            return
        shell(
            "curl -fsSL https://repo.vivaldi.com/archive/linux_signing_key.pub | "
            "sudo gpg --dearmor -o /etc/apt/keyrings/vivaldi.gpg && "
            "echo 'deb [arch=amd64 signed-by=/etc/apt/keyrings/vivaldi.gpg] "
            "https://repo.vivaldi.com/archive/deb/ stable main' | "
            "sudo tee /etc/apt/sources.list.d/vivaldi.list > /dev/null && "
            "sudo apt-get update"
        )

def setup_temurin_repo() -> None:
    # Adoptium uses $basearch in baseurl → multi-arch.
    if PKG_MGR == "dnf":
        if _repo_file_exists("/etc/yum.repos.d/adoptium.repo"):
            return
        _write_dnf_repo(
            "Adoptium", "Adoptium",
            "https://packages.adoptium.net/artifactory/rpm/fedora/$releasever/$basearch",
            "https://packages.adoptium.net/artifactory/api/gpg/key/public",
        )
    elif PKG_MGR == "apt-get":
        if _repo_file_exists("/etc/apt/sources.list.d/adoptium.list"):
            return
        shell(
            "wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | "
            "sudo gpg --dearmor | sudo tee /etc/apt/keyrings/adoptium.gpg > /dev/null && "
            "echo \"deb [signed-by=/etc/apt/keyrings/adoptium.gpg] "
            "https://packages.adoptium.net/artifactory/deb/ "
            "$(awk -F= '/^VERSION_CODENAME/{print$2}' /etc/os-release) main\" | "
            "sudo tee /etc/apt/sources.list.d/adoptium.list > /dev/null && "
            "sudo apt-get update"
        )

_REPO_GROUPS: list[tuple[set[str], callable]] = [
    (
        {"containerd.io", "docker-buildx-plugin", "docker-ce-cli",
         "docker-ce-rootless-extras", "docker-ce", "docker-compose-plugin"},
        setup_docker_repo,
    ),
    ({"gh"}, setup_gh_repo),
    ({"google-chrome-stable"}, setup_chrome_repo),
    ({"vivaldi-stable"}, setup_vivaldi_repo),
    ({"temurin-25-jdk"}, setup_temurin_repo),
]

# ── special package installers ────────────────────────────────────────────────

_SPECIAL_PKGS = {"github-desktop", "zoom", "obsidian", "minikube", "bashtop", "pipx", "poetry"}

# GUI apps — skipped when --no-gui is passed (headless environments).
_GUI_SYSTEM_PKGS = {
    "github-desktop",
    "google-chrome-stable",
    "obs-studio",
    "obsidian",
    "shutter",
    "virt-manager",
    "vivaldi-stable",
    "webcamoid",
    "wireshark",
    "zoom",
}


def _install_github_desktop(tmp: Path) -> None:
    data = _fetch_json("https://api.github.com/repos/shiftkey/desktop/releases/latest")
    if data is None:
        return
    suffix, host_tokens, exclude_tokens = (
        (".rpm", _ARCH_TOKENS[ARCH], _ARCH_TOKENS[_other_arch()])
        if PKG_MGR == "dnf"
        else (".deb", _ARCH_TOKENS[ARCH], _ARCH_TOKENS[_other_arch()])
    )

    def matches(name: str) -> bool:
        n = name.lower()
        if not n.endswith(suffix):
            return False
        if not any(t in n for t in host_tokens):
            return False
        if any(t in n for t in exclude_tokens if t not in host_tokens):
            return False
        return True

    asset = next((a for a in data["assets"] if matches(a["name"])), None)
    if asset is None:
        err(f"No GitHub Desktop {suffix} asset found for {ARCH}")
        return
    dest = tmp / asset["name"]
    if not _download(asset["browser_download_url"], dest):
        return
    installer = "dnf" if PKG_MGR == "dnf" else "apt-get"
    run([installer, "install", "-y", str(dest)], as_sudo=True, check=False)


def _install_zoom(tmp: Path) -> None:
    if ARCH != "x86_64":
        warn("Zoom has no aarch64 Linux client — skipping")
        return
    if PKG_MGR == "dnf":
        dest = tmp / "zoom.rpm"
        if not _download("https://zoom.us/client/latest/zoom_x86_64.rpm", dest):
            return
        run(["dnf", "install", "-y", str(dest)], as_sudo=True, check=False)
    elif PKG_MGR == "apt-get":
        dest = tmp / "zoom.deb"
        if not _download("https://zoom.us/client/latest/zoom_amd64.deb", dest):
            return
        run(["apt-get", "install", "-y", str(dest)], as_sudo=True, check=False)


def _install_obsidian(tmp: Path) -> None:
    data = _fetch_json("https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest")
    if data is None:
        return
    host_tokens = _ARCH_TOKENS[ARCH]
    other_tokens = _ARCH_TOKENS[_other_arch()]

    def matches(name: str) -> bool:
        n = name.lower()
        if not n.endswith(".appimage"):
            return False
        if not any(t in n for t in host_tokens):
            return False
        if any(t in n for t in other_tokens if t not in host_tokens):
            return False
        return True

    asset = next((a for a in data["assets"] if matches(a["name"])), None)
    if asset is None:
        err(f"No Obsidian AppImage found for {ARCH}")
        return
    dest = tmp / asset["name"]
    if not _download(asset["browser_download_url"], dest):
        return
    install_path = Path("/usr/local/bin/obsidian")
    run(["cp", str(dest), str(install_path)], as_sudo=True)
    run(["chmod", "755", str(install_path)], as_sudo=True)
    print(f"  Obsidian AppImage installed at {install_path}")


def _install_minikube(tmp: Path) -> None:
    arch_token = _ARCH_MINIKUBE[ARCH]
    base_url = f"https://storage.googleapis.com/minikube/releases/latest/minikube-linux-{arch_token}"
    dest = tmp / "minikube"
    if not _download(base_url, dest):
        return
    print("  Fetching SHA256 ...")
    try:
        with urllib.request.urlopen(base_url + ".sha256") as resp:
            expected = resp.read().decode().strip().split()[0]
    except (urllib.error.URLError, OSError) as e:
        err(f"minikube SHA256 fetch failed: {e}")
        return
    actual = _sha256_of(dest)
    if actual != expected:
        err(f"minikube SHA256 mismatch: expected {expected}, got {actual}")
        return
    print("  SHA256 OK")
    install_path = Path("/usr/local/bin/minikube")
    run(["cp", str(dest), str(install_path)], as_sudo=True)
    run(["chmod", "755", str(install_path)], as_sudo=True)
    print(f"  minikube installed to {install_path}")


def _install_bashtop(_tmp: Path) -> None:
    clone_dir = Path.home() / "bashtop"
    if clone_dir.exists():
        print(f"  Updating existing clone at {clone_dir} ...")
        if run(["git", "-C", str(clone_dir), "pull"], check=False).returncode != 0:
            err("bashtop git pull failed")
            return
    else:
        print(f"  Cloning bashtop to {clone_dir} ...")
        if run(["git", "clone", "https://github.com/aristocratos/bashtop.git",
                str(clone_dir)], check=False).returncode != 0:
            err("bashtop git clone failed")
            return
    if run(["make", "install"], as_sudo=True, cwd=str(clone_dir), check=False).returncode != 0:
        err("bashtop 'make install' failed")
        return
    # Also expose the clone dir on PATH so `bashtop` from source works.
    profile_line = f"export PATH=$PATH:{clone_dir}"
    profile_script = "/etc/profile.d/bashtop.sh"
    run(["bash", "-c",
         f"grep -qxF {profile_line!r} {profile_script} 2>/dev/null || "
         f"echo {profile_line!r} >> {profile_script}"],
        as_sudo=True, check=False)
    print(f"  bashtop installed. Clone at {clone_dir}, binary at /usr/local/bin/bashtop")


def _install_pipx(_tmp: Path) -> None:
    if not has_cmd("python3"):
        err("Python 3 is not installed — cannot install pipx")
        return
    run([PKG_MGR, "install", "-y", "pipx"], as_sudo=True, check=False)
    if has_cmd("pipx"):
        run(["pipx", "ensurepath"], check=False)
    else:
        err("pipx command not found after install")


def _install_poetry(_tmp: Path) -> None:
    if not has_cmd("pipx"):
        err("pipx is not installed — cannot install poetry")
        return
    run(["pipx", "install", "poetry"], check=False)


def install_special_pkg(pkg: str, tmp: Path) -> None:
    if pkg == "github-desktop":
        _install_github_desktop(tmp)
    elif pkg == "zoom":
        _install_zoom(tmp)
    elif pkg == "obsidian":
        _install_obsidian(tmp)
    elif pkg == "minikube":
        _install_minikube(tmp)
    elif pkg == "bashtop":
        _install_bashtop(tmp)
    elif pkg == "pipx":
        _install_pipx(tmp)
    elif pkg == "poetry":
        _install_poetry(tmp)

# ── system package installation ───────────────────────────────────────────────

def install_system_packages(to_install_regular: list[str], to_install_special: list[str]) -> None:
    print("\n=== System Packages ===")

    seen_repos: set[int] = set()
    for pkg in to_install_regular:
        for idx, (members, setup_fn) in enumerate(_REPO_GROUPS):
            if pkg in members and idx not in seen_repos:
                print(f"  [REPO] Setting up repository for {pkg} ...")
                setup_fn()
                seen_repos.add(idx)

    for pkg in to_install_regular:
        result = run([PKG_MGR, "install", "-y", pkg], as_sudo=True, check=False)
        if result.returncode != 0:
            err(f"System package failed to install: {pkg}")

    if to_install_special:
        with tempfile.TemporaryDirectory() as tmp:
            for pkg in to_install_special:
                print(f"\n  [SPECIAL] Installing {pkg} ...")
                install_special_pkg(pkg, Path(tmp))

# ── flatpak package installation ──────────────────────────────────────────────

def install_flatpak_packages(to_install: list[str]) -> None:
    print("\n=== Flatpak Packages ===")

    if not has_cmd("flatpak"):
        print("  flatpak is not installed.")
        if not _yn("  Install flatpak now? [y/N] "):
            warn("flatpak not installed — skipping Flatpak section")
            return
        result = run([PKG_MGR, "install", "-y", "flatpak"], as_sudo=True, check=False)
        if result.returncode != 0 or not has_cmd("flatpak"):
            err("flatpak installation failed — skipping Flatpak section")
            return

    run(
        ["flatpak", "remote-add", "--if-not-exists", "flathub",
         "https://dl.flathub.org/repo/flathub.flatpakrepo"],
        as_sudo=True, check=False,
    )

    for pkg_id in to_install:
        print(f"\n  Installing {pkg_id} ...")
        result = run(["flatpak", "install", "--noninteractive", "flathub", pkg_id], check=False)
        if result.returncode != 0:
            err(f"Flatpak failed to install: {pkg_id}")

# ── custom package installation ───────────────────────────────────────────────

@dataclass
class CustomPackage:
    name: str
    version: Optional[str] = None              # pinned fallback version
    url_template: Optional[str] = None         # uses {version}, {arch}, {arch_go}
    sha256: Optional[str] = None               # hex digest of the pinned archive
    sha256_url_template: Optional[str] = None  # template for a .minisig URL
    minisign_key: Optional[str] = None         # base64 public key for minisign verification
    fetch_latest: Optional[str] = None         # latest-version resolver hint
    install_path: Optional[str] = None         # override the default install-check path

    @property
    def url(self) -> Optional[str]:
        return _url_format(self.url_template, self.version) if self.url_template else None

    @property
    def sha256_url(self) -> Optional[str]:
        return (
            _url_format(self.sha256_url_template, self.version)
            if self.sha256_url_template else None
        )

    @property
    def display_name(self) -> str:
        return f"{self.name}-{self.version}" if self.version else self.name


_DEFAULT_INSTALL_PATHS: dict[str, Path] = {
    "go":          Path("/usr/local/go"),
    "firecracker": Path("/usr/local/bin/firecracker"),
    "zig":         Path("/usr/local/bin/zig"),
    "nvm":         Path("~/.nvm"),
    "pyenv":       Path("~/.pyenv"),
    "neovim":      Path(f"/opt/nvim-linux-{_ARCH_NVIM[ARCH]}"),
    "oh-my-zsh":   Path("~/.oh-my-zsh"),
}

def _default_install_path(pkg: CustomPackage) -> Optional[Path]:
    return _DEFAULT_INSTALL_PATHS.get(pkg.name.lower())


def _pip_installed() -> bool:
    if not has_cmd("python3"):
        return False
    return subprocess.run(
        ["python3", "-m", "pip", "--version"],
        capture_output=True, check=False,
    ).returncode == 0


def is_custom_pkg_installed(pkg: CustomPackage) -> tuple[bool, Optional[Path]]:
    """Return (is_installed, check_path).

    For most custom packages the check is a filesystem path. ``pip`` is the
    exception: it ships inside a Python distribution rather than at a known
    path, so it's detected by running ``python3 -m pip --version``.
    """
    if pkg.name.lower() == "pip":
        return _pip_installed(), None
    raw = Path(pkg.install_path) if pkg.install_path else _default_install_path(pkg)
    if raw is None:
        return False, None
    check = raw.expanduser()
    return check.exists(), check


def _sha256_of(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def _verify(archive: Path, pkg: CustomPackage) -> bool:
    """Returns True if verification passed (or nothing to verify), False on failure."""
    if pkg.sha256:
        actual = _sha256_of(archive)
        if actual != pkg.sha256:
            err(f"SHA256 mismatch for {pkg.name}: expected {pkg.sha256}, got {actual}")
            return False
        print("  SHA256 OK")
    elif pkg.sha256_url:
        sig_path = archive.parent / Path(pkg.sha256_url).name
        if not _download(pkg.sha256_url, sig_path):
            return False
        if has_cmd("minisign"):
            cmd = ["minisign", "-Vm", str(archive), "-x", str(sig_path)]
            if pkg.minisign_key:
                cmd += ["-P", pkg.minisign_key]
            result = run(cmd, check=False)
            if result.returncode != 0:
                err(f"minisign verification failed for {pkg.name}")
                return False
            print("  minisign OK")
        else:
            warn(f"minisign not installed — skipping signature verification for {pkg.name}")
    return True


def _url_arch_ok(pkg: CustomPackage) -> bool:
    """If the URL clearly targets a different arch than the host, warn and return False."""
    if not pkg.url:
        return True
    if _arch_matches(pkg.url):
        return True
    if _has_other_arch_token(pkg.url):
        warn(
            f"{pkg.name}: URL targets {_other_arch()} but host is {ARCH}. "
            f"Update formatted_packages.txt with a matching URL/SHA256."
        )
        return False
    return True   # ambiguous — let it proceed


def _install_go(archive: Path) -> None:
    go_root = Path("/usr/local/go")
    if go_root.exists():
        print(f"  Removing existing Go at {go_root} ...")
        run(["rm", "-rf", str(go_root)], as_sudo=True)
    run(["tar", "-C", "/usr/local", "-xzf", str(archive)], as_sudo=True)
    profile_line = "export PATH=$PATH:/usr/local/go/bin"
    profile_script = "/etc/profile.d/local_go.sh"
    run(["bash", "-c",
         f"grep -qxF {profile_line!r} {profile_script} 2>/dev/null || "
         f"echo {profile_line!r} >> {profile_script}"],
        as_sudo=True, check=False)
    print(f"  Go installed to {go_root}")


def _install_firecracker(archive: Path, tmp: Path) -> None:
    with tarfile.open(archive) as tf:
        tf.extractall(tmp, filter="data")
    binary = next(
        (p for p in tmp.rglob("firecracker*")
         if p.is_file() and not p.suffix == ".debug" and "debug" not in p.name),
        None,
    )
    if binary is None:
        err("firecracker binary not found in archive")
        return
    dest = Path("/usr/local/bin/firecracker")
    run(["cp", str(binary), str(dest)], as_sudo=True)
    run(["chmod", "755", str(dest)], as_sudo=True)
    print(f"  firecracker installed to {dest}")


def _install_zig(pkg: CustomPackage, archive: Path, tmp: Path) -> None:
    parent = Path("/usr/local")
    zig_dir = parent / f"zig-{pkg.version}"
    if zig_dir.exists():
        run(["rm", "-rf", str(zig_dir)], as_sudo=True)
    run(["tar", "-C", str(parent), "-xJf", str(archive)], as_sudo=True)
    extracted = next(parent.glob(f"zig-{ARCH}-linux*"), None)
    if extracted and extracted != zig_dir:
        run(["mv", str(extracted), str(zig_dir)], as_sudo=True)
    symlink = Path("/usr/local/bin/zig")
    run(["ln", "-sf", str(zig_dir / "zig"), str(symlink)], as_sudo=True)
    print(f"  Zig installed to {zig_dir}, symlinked at {symlink}")


def _install_pyenv() -> None:
    print("  Installing pyenv via curl ...")
    if shell("curl https://pyenv.run | bash", check=False).returncode != 0:
        err("pyenv installation failed")
        return
    print("  pyenv installed to ~/.pyenv")


def _install_pip() -> None:
    """Install pip via Python's bundled ``ensurepip`` module, then self-upgrade.

    Unlike the other custom packages, pip ships inside CPython itself and is
    bootstrapped from the wheel in the standard library rather than downloaded.
    """
    if not has_cmd("python3"):
        err("python3 is not installed — cannot install pip")
        return
    print("  Bootstrapping pip via 'python3 -m ensurepip --upgrade' ...")
    bootstrap = run(
        ["python3", "-m", "ensurepip", "--upgrade"],
        as_sudo=True, check=False,
    )
    if bootstrap.returncode != 0:
        err("python3 -m ensurepip failed (system Python may need a distro 'python3-pip' package)")
        return
    print("  Upgrading pip to the latest version ...")
    upgrade = run(
        ["python3", "-m", "pip", "install", "--upgrade", "pip"],
        as_sudo=True, check=False,
    )
    if upgrade.returncode != 0:
        warn("pip self-upgrade failed (likely PEP 668 externally-managed); "
             "ensurepip-provided pip remains")


def _install_oh_my_zsh() -> None:
    """Install oh-my-zsh via its official installer and force ZSH_THEME=gnzh."""
    if not has_cmd("zsh"):
        err("zsh is not installed — required by oh-my-zsh")
        return
    if not has_cmd("git"):
        err("git is not installed — required by oh-my-zsh")
        return

    target = Path.home() / ".oh-my-zsh"
    if target.exists():
        print(f"  oh-my-zsh already present at {target}; updating theme only")
    else:
        print("  Installing oh-my-zsh via the official installer ...")
        installer = (
            'sh -c "$(curl -fsSL '
            'https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" '
            '"" --unattended'
        )
        if shell(installer, check=False).returncode != 0:
            err("oh-my-zsh installer failed")
            return

    zshrc = Path.home() / ".zshrc"
    if not zshrc.exists():
        warn("~/.zshrc not present after oh-my-zsh install; cannot set theme")
        return

    text = zshrc.read_text()
    new_text, replaced = re.subn(r'^\s*ZSH_THEME=.*$', 'ZSH_THEME="gnzh"', text, flags=re.M)
    if replaced == 0:
        new_text = text.rstrip() + '\nZSH_THEME="gnzh"\n'
    if new_text != text:
        zshrc.write_text(new_text)
        print('  Set ZSH_THEME="gnzh" in ~/.zshrc')
    else:
        print('  ~/.zshrc already has ZSH_THEME="gnzh"')


def _install_nvm() -> None:
    data = _fetch_json("https://api.github.com/repos/nvm-sh/nvm/releases/latest")
    if data is None:
        return
    version = data["tag_name"]
    install_url = f"https://raw.githubusercontent.com/nvm-sh/nvm/{version}/install.sh"
    print(f"  Installing NVM {version} via curl ...")
    if shell(f"curl -o- {install_url} | bash", check=False).returncode != 0:
        err("NVM installation failed")
        return
    print(f"  NVM {version} installed to ~/.nvm")


def _install_neovim(pkg: CustomPackage, tmp: Path) -> None:
    data = _fetch_json("https://api.github.com/repos/neovim/neovim/releases/latest")
    if data is None:
        return

    arch_token = _ARCH_NVIM[ARCH]
    asset_name = f"nvim-linux-{arch_token}.tar.gz"
    asset = next((a for a in data["assets"] if a["name"] == asset_name), None)
    if asset is None:
        err(f"Neovim asset {asset_name} not found")
        return

    expected_digest = asset.get("digest")
    if not expected_digest or not expected_digest.startswith("sha256:"):
        err("Neovim asset digest missing or invalid")
        return
    expected_hash = expected_digest.split(":", 1)[1]

    dest = tmp / asset_name
    if not _download(asset["browser_download_url"], dest):
        return

    actual_hash = _sha256_of(dest)
    if actual_hash != expected_hash:
        err(f"Neovim SHA256 mismatch: expected {expected_hash}, got {actual_hash}")
        return
    print("  SHA256 OK")

    install_dir = f"/opt/nvim-linux-{arch_token}"
    print(f"  Extracting Neovim to /opt ...")
    run(["rm", "-rf", install_dir], as_sudo=True)
    run(["tar", "-C", "/opt", "-xzf", str(dest)], as_sudo=True)

    profile_line = f'export PATH="$PATH:{install_dir}/bin"'
    profile_script = "/etc/profile.d/neovim.sh"
    run(["bash", "-c",
         f"grep -qxF {profile_line!r} {profile_script} 2>/dev/null || "
         f"echo {profile_line!r} >> {profile_script}"],
        as_sudo=True, check=False)
    print(f"  Neovim installed to {install_dir}")


def _clone_nvim_config() -> None:
    config_dir = Path.home() / ".config" / "nvim"
    repo_url = "git@github.com:JMR-dev/nvim-config.git"

    print(f"\n[Neovim] Setting up configuration from {repo_url} ...")

    if config_dir.exists():
        print(f"  Removing existing configuration at {config_dir} ...")
        shutil.rmtree(config_dir)

    config_dir.parent.mkdir(parents=True, exist_ok=True)

    print(f"  Cloning to {config_dir} ...")
    # We use a temp clone and then move to ensure we handle the "rename" part of the request
    # although cloning directly to 'nvim' is effectively the same.
    # The user asked: "clones my nvim config to $HOME/.config/$REPO and renames the repo root folder to just nvim"
    repo_name = repo_url.split("/")[-1].replace(".git", "")
    temp_clone = config_dir.parent / repo_name

    if temp_clone.exists():
        shutil.rmtree(temp_clone)

    result = run(["git", "clone", repo_url, str(temp_clone)], check=False)
    if result.returncode != 0:
        err("Neovim configuration clone failed")
        return

    print(f"  Renaming {temp_clone.name} to {config_dir.name} ...")
    temp_clone.rename(config_dir)
    print(f"  Neovim configuration ready at {config_dir}")


def _invoking_user() -> str:
    """User whose login shell / home we should target.

    When the script is run via sudo, SUDO_USER is the original invoker;
    otherwise the current process user is correct.
    """
    return os.environ.get("SUDO_USER") or getpass.getuser()


def ensure_zsh_default() -> None:
    """Make zsh the default login shell for the invoking user.

    Uses ``usermod -s`` on RHEL-family distros and ``chsh -s`` elsewhere —
    on Debian/Ubuntu ``chsh`` is the canonical (and PAM-permitted) path,
    while on RHEL/Fedora ``chsh`` for another user often fails under the
    default authselect config and ``usermod`` is the reliable alternative.
    """
    if not has_cmd("zsh"):
        warn("zsh not installed — skipping default-shell change")
        return

    zsh_path = shutil.which("zsh") or "/bin/zsh"
    user = _invoking_user()

    import pwd
    try:
        current = pwd.getpwnam(user).pw_shell
    except KeyError:
        warn(f"user {user} not found in passwd; skipping default-shell change")
        return

    if current == zsh_path:
        print(f"\n[zsh] {user}'s default shell is already {zsh_path}.")
        return

    family = "RHEL-family" if IS_RHEL_FAMILY else "Debian-family"
    print(f"\n[zsh] Setting default shell for {user} to {zsh_path} ({family}) ...")

    if IS_RHEL_FAMILY:
        cmd = ["usermod", "-s", zsh_path, user]
    else:
        cmd = ["chsh", "-s", zsh_path, user]

    if run(cmd, as_sudo=True, check=False).returncode != 0:
        err(f"Failed to set default shell to zsh for {user}")
    else:
        print(f"[zsh] Default shell updated. Log out and back in for it to take effect.")


def ensure_node_lts() -> None:
    """If nvm is present, ensure Node LTS is installed and set as the default."""
    nvm_dir = Path.home() / ".nvm"
    if not nvm_dir.exists():
        return
    # nvm version lts/* prints the installed LTS version, or "N/A" if not installed
    check = shell(
        'bash -c "source ~/.nvm/nvm.sh 2>/dev/null && nvm version lts/* 2>/dev/null"',
        capture_output=True, text=True, check=False,
    )
    installed = check.stdout.strip()
    if installed and installed != "N/A":
        print(f"\n[NVM] Node LTS ({installed}) already installed.")
    else:
        print("\n[NVM] Installing Node.js LTS ...")
        result = shell('bash -c "source ~/.nvm/nvm.sh && nvm install --lts"', check=False)
        if result.returncode != 0:
            err("Node.js LTS install via nvm failed")
            return
        print("  Node.js LTS installed.")

    print("[NVM] Setting Node LTS as default ...")
    result = shell(
        "bash -c \"source ~/.nvm/nvm.sh && nvm alias default 'lts/*' && nvm use --lts\"",
        check=False,
    )
    if result.returncode != 0:
        err("Setting nvm default to LTS failed")


def _latest_stable_python(pyenv_bin: Path) -> Optional[str]:
    """Return the latest stable CPython 3.x version string from `pyenv install --list`."""
    result = subprocess.run(
        [str(pyenv_bin), "install", "--list"],
        capture_output=True, text=True, check=False,
    )
    if result.returncode != 0:
        err("pyenv install --list failed")
        return None
    # Match only pure X.Y.Z lines — excludes a1/b1/rc1/dev suffixes and PyPy/Anaconda/etc.
    stable_re = re.compile(r"^\s*(\d+)\.(\d+)\.(\d+)\s*$")
    versions: list[tuple[int, int, int]] = []
    for line in result.stdout.splitlines():
        m = stable_re.match(line)
        if m:
            major, minor, patch = int(m.group(1)), int(m.group(2)), int(m.group(3))
            if major >= 3:
                versions.append((major, minor, patch))
    if not versions:
        return None
    versions.sort()
    return ".".join(str(p) for p in versions[-1])


def ensure_python_latest() -> Optional[threading.Thread]:
    """If pyenv is present, ensure the latest stable Python is installed and set as global.

    When a compile is required, runs it in a background thread and returns the
    thread handle. The caller must `join()` it before writing the run log so any
    install/global failure is captured. Returns None if no compile was needed.
    """
    pyenv_dir = Path.home() / ".pyenv"
    if not pyenv_dir.exists():
        return None
    pyenv_bin = pyenv_dir / "bin" / "pyenv"
    if not pyenv_bin.exists():
        warn(f"pyenv binary not found at {pyenv_bin}")
        return None

    latest = _latest_stable_python(pyenv_bin)
    if latest is None:
        err("Could not determine latest stable Python from pyenv")
        return None

    installed = subprocess.run(
        [str(pyenv_bin), "versions", "--bare"],
        capture_output=True, text=True, check=False,
    ).stdout.split()

    if latest in installed:
        # Fast path — no compile needed, just set global synchronously.
        print(f"\n[pyenv] Python {latest} already installed.")
        print(f"[pyenv] Setting Python {latest} as global default ...")
        if run([str(pyenv_bin), "global", latest], check=False).returncode != 0:
            err(f"pyenv global {latest} failed")
        return None

    print(f"\n[pyenv] Backgrounding install of Python {latest} "
          f"(compile may take several minutes; output captured) ...")
    start = time.monotonic()

    def _worker():
        install_cmd = [str(pyenv_bin), "install", "--skip-existing", latest]
        p1 = subprocess.run(install_cmd, capture_output=True, text=True, check=False)
        elapsed = int(time.monotonic() - start)
        if p1.returncode != 0:
            err(f"pyenv install {latest} failed after {elapsed}s")
            tail = "\n".join(p1.stderr.splitlines()[-20:]) if p1.stderr else ""
            if tail:
                print(f"\n[pyenv stderr tail]\n{tail}")
            return
        p2 = subprocess.run(
            [str(pyenv_bin), "global", latest],
            capture_output=True, text=True, check=False,
        )
        if p2.returncode != 0:
            err(f"pyenv global {latest} failed")
            return
        print(f"\n[pyenv] Python {latest} installed and set as global default ({elapsed}s).")

    t = threading.Thread(target=_worker, daemon=True, name="pyenv-install")
    t.start()
    return t


def _resolve_latest_go(pkg: CustomPackage) -> Optional[tuple[str, str]]:
    releases = _fetch_json("https://go.dev/dl/?mode=json")
    if not releases:
        return None
    latest = releases[0] if isinstance(releases, list) else releases
    raw_version = latest.get("version", "")
    version = raw_version[2:] if raw_version.startswith("go") else raw_version
    if not version:
        return None
    archive_name = f"go{version}.linux-{_ARCH_GO[ARCH]}.tar.gz"
    entry = next(
        (f for f in latest.get("files", [])
         if f.get("filename") == archive_name and f.get("kind") == "archive"),
        None,
    )
    if not entry or not entry.get("sha256"):
        return None
    return version, entry["sha256"]


def _resolve_latest_firecracker(pkg: CustomPackage) -> Optional[tuple[str, str]]:
    data = _fetch_json(
        "https://api.github.com/repos/firecracker-microvm/firecracker/releases/latest"
    )
    if not data:
        return None
    version = data.get("tag_name", "").lstrip("v")
    if not version:
        return None
    archive_name = f"firecracker-v{version}-{ARCH}.tgz"
    sha_asset = next(
        (a for a in data.get("assets", []) if a["name"] == f"{archive_name}.sha256.txt"),
        None,
    )
    if not sha_asset:
        return None
    sha = _fetch_text(sha_asset["browser_download_url"])
    if not sha:
        return None
    return version, sha.split()[0]


def _resolve_latest_zig(_pkg: CustomPackage) -> Optional[tuple[str, str]]:
    data = _fetch_json("https://ziglang.org/download/index.json")
    if not data:
        return None
    stable = [v for v in data.keys() if v != "master" and re.match(r"^\d+\.\d+\.\d+$", v)]
    if not stable:
        return None
    stable.sort(key=lambda v: tuple(int(x) for x in v.split(".")))
    version = stable[-1]
    entry = data[version].get(f"{ARCH}-linux")
    if not entry or "shasum" not in entry:
        return None
    return version, entry["shasum"]


_LATEST_RESOLVERS = {
    "go":          _resolve_latest_go,
    "firecracker": _resolve_latest_firecracker,
    "zig":         _resolve_latest_zig,
}


def _resolve_latest(pkg: CustomPackage) -> None:
    """Best-effort upgrade pkg.version/sha256 to the latest release.

    On any failure, logs a warning and leaves the pinned values in place.
    Clears sha256_url_template when sha256 is overridden so the dynamic
    digest is what gets verified.
    """
    resolver = _LATEST_RESOLVERS.get(pkg.fetch_latest or "")
    if resolver is None:
        return
    print(f"  Checking latest version for {pkg.name} ...")
    try:
        result = resolver(pkg)
    except Exception as e:  # noqa: BLE001 — best-effort lookup, any failure is logged
        warn(f"{pkg.name}: latest-version lookup raised {e!r}; "
             f"falling back to pinned version {pkg.version}")
        return
    if result is None:
        warn(f"{pkg.name}: could not resolve latest version; "
             f"falling back to pinned version {pkg.version}")
        return
    latest_version, latest_sha = result
    if latest_version == pkg.version:
        print(f"  Pinned version {pkg.version} is already the latest.")
        return
    print(f"  Latest is {latest_version} (pinned was {pkg.version}); using latest.")
    pkg.version = latest_version
    pkg.sha256 = latest_sha
    pkg.sha256_url_template = None  # prefer the freshly resolved sha256


def install_custom_packages(to_install: list[CustomPackage]) -> None:
    print("\n=== Custom Packages ===")
    for pkg in to_install:
        name_lower = pkg.name.lower()
        _, check_path = is_custom_pkg_installed(pkg)
        print(f"\n  Installing {pkg.display_name} ..."
              + (f" (install path: {check_path})" if check_path else ""))
        if check_path is None and name_lower != "pip":
            warn(f"{pkg.name}: no known install path — script will not detect future installs")

        # Handlers that manage their own download/install
        if name_lower == "nvm":
            _install_nvm()
            continue
        if name_lower == "pyenv":
            _install_pyenv()
            continue
        if name_lower == "pip":
            _install_pip()
            continue
        if name_lower == "oh-my-zsh":
            _install_oh_my_zsh()
            continue
        if name_lower == "neovim":
            with tempfile.TemporaryDirectory() as tmp_str:
                _install_neovim(pkg, Path(tmp_str))
            continue

        _resolve_latest(pkg)

        if not pkg.url:
            warn(f"No URL or install handler for '{pkg.name}' — skipping")
            continue

        if not _url_arch_ok(pkg):
            continue

        with tempfile.TemporaryDirectory() as tmp_str:
            tmp = Path(tmp_str)
            archive = tmp / Path(pkg.url).name
            if not _download(pkg.url, archive):
                continue
            if not _verify(archive, pkg):
                continue
            if name_lower == "go":
                _install_go(archive)
            elif name_lower == "firecracker":
                _install_firecracker(archive, tmp)
            elif name_lower == "zig":
                _install_zig(pkg, archive, tmp)
            else:
                warn(f"No install handler for '{pkg.name}' — skipping")

# ── pre-install checks ───────────────────────────────────────────────────────

def check_system_packages(names: list[str]) -> dict:
    overrides = _OVERRIDES.get(PKG_MGR, {})
    resolved, skipped = resolve_system_pkgs(names)
    # Packages remapped to different name(s) (not skipped entirely)
    remapped = [(n, overrides[n]) for n in names if n in overrides and overrides[n] is not None]

    special = [p for p in resolved if p in _SPECIAL_PKGS]
    regular  = [p for p in resolved if p not in _SPECIAL_PKGS]

    to_install_r, already_r = [], []
    for p in regular:
        (already_r if is_system_pkg_installed(p) else to_install_r).append(p)

    to_install_s, already_s = [], []
    for p in special:
        (already_s if is_special_pkg_installed(p) else to_install_s).append(p)

    return {
        "to_install_regular": to_install_r,
        "to_install_special": to_install_s,
        "already_installed":  already_r + already_s,
        "skipped":            skipped,
        "remapped":           remapped,
    }


def check_flatpak_packages(pkg_ids: list[str]) -> dict:
    to_install, already = [], []
    for p in pkg_ids:
        (already if is_flatpak_installed(p) else to_install).append(p)
    return {"to_install": to_install, "already_installed": already}


def check_custom_packages(packages: list[CustomPackage]) -> dict:
    to_install, already = [], []
    for pkg in packages:
        installed, path = is_custom_pkg_installed(pkg)
        (already if installed else to_install).append((pkg, path))
    return {"to_install": [p for p, _ in to_install],
            "already_installed": already}


def _fmt(items: list, limit: int = 6) -> str:
    names = [str(i) for i in items]
    shown = " ".join(names[:limit])
    return shown + (f"  … +{len(names)-limit} more" if len(names) > limit else "")


def print_check_summary(sys_c: dict, flat_c: dict, cust_c: dict, only: Optional[str]) -> int:
    """Print pre-install summary. Returns total count to install."""
    total = 0

    if only in (None, "system"):
        to_r  = sys_c["to_install_regular"]
        to_s  = sys_c["to_install_special"]
        ok    = sys_c["already_installed"]
        skip  = sys_c["skipped"]
        remap = sys_c["remapped"]
        print("\nSystem packages:")
        if ok:
            print(f"  [OK]      {len(ok):3d} already installed")
        n = len(to_r) + len(to_s)
        if n:
            print(f"  [INSTALL] {n:3d} to install:  {_fmt(to_r + to_s)}")
        if skip:
            print(f"  [SKIP]    {len(skip):3d} overridden (→ skip): {_fmt(skip)}")
        if remap:
            pairs = "  ".join(f"{a}→{','.join(b)}" for a, b in remap)
            print(f"  [REMAP]       remapped: {pairs}")
        total += n

    if only in (None, "flatpak") and flat_c:
        to   = flat_c["to_install"]
        ok   = flat_c["already_installed"]
        print("\nFlatpak packages:")
        if ok:
            print(f"  [OK]      {len(ok):3d} already installed")
        if to:
            print(f"  [INSTALL] {len(to):3d} to install:  {_fmt(to)}")
        total += len(to)

    if only in (None, "custom"):
        to = cust_c["to_install"]
        ok = cust_c["already_installed"]
        print("\nCustom packages:")
        for pkg, path in ok:
            print(f"  [OK]      {pkg.display_name}" + (f"  ({path})" if path else ""))
        for pkg in to:
            _, path = is_custom_pkg_installed(pkg)
            print(f"  [INSTALL] {pkg.display_name}" + (f"  → {path}" if path else ""))
        total += len(to)

    return total

# ── ssh key + github auth ─────────────────────────────────────────────────────

def _yn(prompt: str) -> bool:
    """Ask a y/N question. Returns True only for 'y'."""
    try:
        return input(prompt).strip().lower() == "y"
    except (EOFError, KeyboardInterrupt):
        print()
        return False


def _gh_logged_in() -> bool:
    return subprocess.run(
        ["gh", "auth", "status"], capture_output=True, check=False
    ).returncode == 0


def _offer_github_upload(pub_keys: list[Path]) -> None:
    if not pub_keys:
        print("  No public key found to upload.")
        return

    pub_key = max(pub_keys, key=lambda p: p.stat().st_mtime)
    if not _yn(f"\n  Upload {pub_key.name} to your GitHub profile? [y/N] "):
        return

    default_title = f"{getpass.getuser()}@{os.uname().nodename}"
    try:
        title = input(f"  Key title [{default_title}]: ").strip() or default_title
    except (EOFError, KeyboardInterrupt):
        title = default_title


def check_and_setup_ssh() -> None:
    if not has_cmd("gh"):
        print("\n[GitHub CLI] gh not installed — skipping authentication.")
        return

    if _gh_logged_in():
        print("\n[GitHub CLI] Already authenticated.")
        return

    if not _yn("\n[GitHub CLI] Would you like to authenticate the GitHub CLI? [y/N] "):
        return

    result = subprocess.run(["gh", "auth", "login"], check=False)
    if result.returncode != 0:
        err("gh auth login failed — skipping key upload.")
        return

# ── packages module loader ────────────────────────────────────────────────────

def load_packages() -> tuple[list[str], list[str], list[CustomPackage]]:
    system_pkgs  = list(formatted_packages.SYSTEM_PACKAGES)
    flatpak_pkgs = list(formatted_packages.FLATPAK_PACKAGES)
    custom_pkgs  = [CustomPackage(**spec) for spec in formatted_packages.CUSTOM_PACKAGES]
    return system_pkgs, flatpak_pkgs, custom_pkgs

# ── entry point ───────────────────────────────────────────────────────────────

def main() -> None:
    ap = argparse.ArgumentParser(
        description="Bootstrap packages declared in formatted_packages.py"
    )
    ap.add_argument("--only", choices=["system", "flatpak", "custom"],
                    help="Install only the named section")
    ap.add_argument("--no-gui", action="store_true",
                    help="Skip GUI applications (suitable for headless environments). "
                         "Excludes GUI system packages and skips the entire Flatpak "
                         "section, including installing flatpak itself.")
    args = ap.parse_args()

    system_pkgs, flatpak_pkgs, custom_pkgs = load_packages()

    print(f"Architecture:    {ARCH}")
    print(f"Package manager: {PKG_MGR}")
    if args.no_gui:
        print("Mode:            headless (--no-gui) — skipping GUI apps and Flatpak")
    print("Checking installed packages ...")

    if args.no_gui:
        skipped_gui = [p for p in system_pkgs if p in _GUI_SYSTEM_PKGS]
        system_pkgs = [p for p in system_pkgs if p not in _GUI_SYSTEM_PKGS]
        if skipped_gui:
            print(f"  [NO-GUI] Skipping GUI system packages: {_fmt(skipped_gui)}")
        flatpak_pkgs = []

    # --no-gui suppresses the Flatpak section entirely (both `flatpak` itself
    # and the Flathub apps), even when --only=flatpak is requested.
    do_flatpak = args.only in (None, "flatpak") and not args.no_gui

    sys_c  = check_system_packages(system_pkgs)   if args.only in (None, "system")  else {}
    flat_c = check_flatpak_packages(flatpak_pkgs) if do_flatpak                     else {}
    cust_c = check_custom_packages(custom_pkgs)   if args.only in (None, "custom")  else {}

    total = print_check_summary(sys_c, flat_c, cust_c, args.only)

    if total == 0:
        print("\nAll packages already installed.")
        write_run_log()
        return

    try:
        answer = input(f"\n{total} item(s) to install. Proceed? [y/N] ").strip().lower()
    except (EOFError, KeyboardInterrupt):
        print()
        sys.exit("Aborted.")
    if answer != "y":
        sys.exit("Aborted.")

    check_sudo()

    if args.only in (None, "system"):
        install_system_packages(sys_c["to_install_regular"], sys_c["to_install_special"])
        ensure_zsh_default()

    if do_flatpak:
        install_flatpak_packages(flat_c["to_install"])

    pyenv_thread: Optional[threading.Thread] = None
    if args.only in (None, "custom"):
        install_custom_packages(cust_c["to_install"])
        ensure_node_lts()
        pyenv_thread = ensure_python_latest()

    if args.only is None:
        check_and_setup_ssh()
        _clone_nvim_config()

    if pyenv_thread is not None:
        if pyenv_thread.is_alive():
            print("\n[pyenv] Waiting for background Python install to finish ...")
        pyenv_thread.join()

    write_run_log()
    print("\nDone.")

    # Final step (user-requested): source ~/.zshrc.
    # This runs in a subshell, so it only validates the rc file — the user's
    # interactive shell is unaffected and they'll need to open a new terminal
    # (or 'exec zsh') to pick up the new default shell.
    zshrc = Path.home() / ".zshrc"
    if has_cmd("zsh") and zshrc.exists():
        print("\nSourcing ~/.zshrc ...")
        shell(f"zsh -c 'source {zshrc}'", check=False)


if __name__ == "__main__":
    main()
