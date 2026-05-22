"""Package definitions consumed by bootstrap_environment.py.

System and Flatpak packages are flat lists of names.

Custom packages declare a URL template plus an optional ``fetch_latest`` hint.
At install time the bootstrap script will attempt to look up the most recent
release and fall back to the pinned (version, sha256) tuple on failure.

URL templates use ``str.format`` with the following substitutions:
    {version}   pkg.version (or the latest resolved version)
    {arch}      "x86_64" or "aarch64"
    {arch_go}   Go-style: "amd64" or "arm64"
    {os}        "linux" or "macos"
    {os_go}     Go-style: "linux" or "darwin"
    {os_zig}    Zig-style: "linux" or "macos"
    {os_nvim}   Neovim-style: "linux" or "macos"
"""

SYSTEM_PACKAGES: list[str] = [
    "ansible",
    "ansible-core",
    "aria2",
    "bashtop",
    "build-essential",
    "buildah",
    "containerd.io",
    "docker-buildx-plugin",
    "docker-ce-cli",
    "docker-ce-rootless-extras",
    "docker-ce",
    "docker-compose-plugin",
    "dotnet-sdk-10.0",
    "ffmpeg-free",
    "gcc",
    "gh",
    "git",
    "github-desktop",
    "google-chrome-stable",
    "lua",
    "minisign",
    "minikube",
    "obs-studio",
    "obsidian",
    "pipx",
    "poetry",
    "podman",
    "qemu",
    "restic",
    "rg",
    "shutter",
    "temurin-25-jdk",
    "vagrant",
    "virt-manager",
    "vivaldi-stable",
    "webcamoid",
    "wireshark",
    "yt-dlp",
    "zoom",
    "zsh",
    "bzip2",
    "bzip2-devel",
    "curl",
    "gdbm-libs",
    "libffi-devel",
    "libnsl2",
    "libuuid-devel",
    "libxml2-devel",
    "libzstd-devel",
    "make",
    "ncurses-devel",
    "openssl-devel",
    "patch",
    "readline-devel",
    "sqlite",
    "sqlite-devel",
    "tk-devel",
    "xmlsec1-devel",
    "xz",
    "xz-devel",
    "zlib-devel",
]

FLATPAK_PACKAGES: list[str] = [
    "com.obsproject.Studio",
    "fr.handbrake.ghb",
    "io.github.webcamoid.Webcamoid",
    "one.ablaze.floorp",
    "com.vivaldi.Vivaldi",
    "org.darktable.Darktable",
]

CUSTOM_PACKAGES: list[dict] = [
    {
        "name": "go",
        "version": "1.26.3",
        "url_template": "https://go.dev/dl/go{version}.{os_go}-{arch_go}.tar.gz",
        "sha256_map": {
            "linux-x86_64":  "2b2cfc7148493da5e73981bffbf3353af381d5f93e789c82c79aff64962eb556",
            "linux-aarch64": "9d89a3ea57d141c2b22d70083f2c8459ba3890f2d9e818e7e933b75614936565",
            "macos-x86_64":  "278d580b32e299fe4a9c990fcf2d02acfe538c7e551a6ee18f9c7164573d2c63",
            "macos-aarch64": "875cf54a15311eee2c99b9dd67c68c4a49351d489ab622bf2cfd28c8f2078d3c",
        },
        "fetch_latest": "go",
    },
    {"name": "neovim"},
    {
        "name": "firecracker",
        "version": "1.15.1",
        "url_template": (
            "https://github.com/firecracker-microvm/firecracker/releases/download/"
            "v{version}/firecracker-v{version}-{arch}.tgz"
        ),
        "sha256_map": {
            "linux-x86_64":  "d4a32ab2322d887ca1bc4a4e7afa9cc35393e6362dfc2b3becb389d362e4275a",
            "linux-aarch64": "00654ac1e702a22744121ea9f10a4f792ebd7c3a744cba587dfac9fcb79b41a5",
        },
        "fetch_latest": "firecracker",
    },
    {
        "name": "zig",
        "version": "0.16.0",
        "url_template": "https://ziglang.org/download/{version}/zig-{arch}-{os_zig}-{version}.tar.xz",
        "sha256_url_template": (
            "https://ziglang.org/download/{version}/zig-{arch}-{os_zig}-{version}.tar.xz.minisig"
        ),
        "minisign_key": "RWSGOq2NVecA2UPNdBUZykf1CCb147pkmdtYxgb3Ti+JO/wCYvhbAb/U",
        "fetch_latest": "zig",
    },
    {"name": "nvm"},
    {"name": "pyenv"},
    {"name": "pip"},
    {"name": "oh-my-zsh"},
]
