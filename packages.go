package main

// Package definitions consumed by the bootstrap entry point.
//
// SystemPackages and FlatpakPackages are flat name lists. CustomPackages
// declare a URL template plus an optional FetchLatest hint; at install time
// the resolver attempts to look up the most recent release and falls back to
// the pinned (Version, sha256) tuple on failure.
//
// URL templates use the substitutions described in formatURL.

var SystemPackages = []string{
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
	"lazygit",
	"lua",
	"minisign",
	"minikube",
	"obs-studio",
	"obsidian",
	"pipx",
	"poetry",
	"pulumi",
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
}

var FlatpakPackages = []string{
	"com.obsproject.Studio",
	"fr.handbrake.ghb",
	"io.github.webcamoid.Webcamoid",
	"one.ablaze.floorp",
	"com.vivaldi.Vivaldi",
	"org.darktable.Darktable",
}

// CustomPackage describes a third-party tarball/binary we fetch directly
// (i.e. not via the host package manager).
type CustomPackage struct {
	Name              string
	Version           string            // pinned fallback version
	URLTemplate       string            // see formatURL for substitutions
	SHA256            string            // single-arch hex digest (set by resolveLatest)
	SHA256Map         map[string]string // per-platform pinned digests: {"os-arch": hex}
	SHA256URLTemplate string            // template for a .minisig URL
	MinisignKey       string            // base64 public key for minisign verification
	FetchLatest       string            // latest-version resolver hint ("go", "firecracker", "zig")
	InstallPath       string            // override the default install-check path
}

func customPackages() []CustomPackage {
	return []CustomPackage{
		{
			Name:        "go",
			Version:     "1.26.3",
			URLTemplate: "https://go.dev/dl/go{version}.{os_go}-{arch_go}.tar.gz",
			SHA256Map: map[string]string{
				"linux-x86_64":  "2b2cfc7148493da5e73981bffbf3353af381d5f93e789c82c79aff64962eb556",
				"linux-aarch64": "9d89a3ea57d141c2b22d70083f2c8459ba3890f2d9e818e7e933b75614936565",
				"macos-x86_64":  "278d580b32e299fe4a9c990fcf2d02acfe538c7e551a6ee18f9c7164573d2c63",
				"macos-aarch64": "875cf54a15311eee2c99b9dd67c68c4a49351d489ab622bf2cfd28c8f2078d3c",
			},
			FetchLatest: "go",
		},
		{Name: "neovim"},
		{
			Name:    "firecracker",
			Version: "1.15.1",
			URLTemplate: "https://github.com/firecracker-microvm/firecracker/releases/download/" +
				"v{version}/firecracker-v{version}-{arch}.tgz",
			SHA256Map: map[string]string{
				"linux-x86_64":  "d4a32ab2322d887ca1bc4a4e7afa9cc35393e6362dfc2b3becb389d362e4275a",
				"linux-aarch64": "00654ac1e702a22744121ea9f10a4f792ebd7c3a744cba587dfac9fcb79b41a5",
			},
			FetchLatest: "firecracker",
		},
		{
			Name:              "zig",
			Version:           "0.16.0",
			URLTemplate:       "https://ziglang.org/download/{version}/zig-{arch}-{os_zig}-{version}.tar.xz",
			SHA256URLTemplate: "https://ziglang.org/download/{version}/zig-{arch}-{os_zig}-{version}.tar.xz.minisig",
			MinisignKey:       "RWSGOq2NVecA2UPNdBUZykf1CCb147pkmdtYxgb3Ti+JO/wCYvhbAb/U",
			FetchLatest:       "zig",
		},
		{Name: "nvm"},
		{Name: "pyenv"},
		{Name: "pip"},
		{Name: "oh-my-zsh"},
		{Name: "agy"},
		{Name: "claude"},
		{Name: "codex"},
		{Name: "copilot"},
	}
}
