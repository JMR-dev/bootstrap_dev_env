package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectOSAndArch(t *testing.T) {
	// detectOS and detectArch call os.Exit on unsupported platforms.
	// Since the tests run on a supported platform, let's verify they return valid values.
	osVal := detectOS()
	if osVal != "linux" && osVal != "macos" {
		t.Errorf("expected linux or macos, got %q", osVal)
	}

	archVal := detectArch()
	if archVal != "x86_64" && archVal != "aarch64" {
		t.Errorf("expected x86_64 or aarch64, got %q", archVal)
	}
}

func TestFormatURL(t *testing.T) {
	defer resetMocks()

	osName = "linux"
	archName = "x86_64"

	tmpl := "https://example.com/download/go-{version}-{os}-{arch}.tar.gz"
	expected := "https://example.com/download/go-1.20-linux-x86_64.tar.gz"
	actual := formatURL(tmpl, "1.20")
	if actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestOtherArch(t *testing.T) {
	defer resetMocks()

	archName = "x86_64"
	if otherArch() != "aarch64" {
		t.Errorf("expected aarch64, got %q", otherArch())
	}

	archName = "aarch64"
	if otherArch() != "x86_64" {
		t.Errorf("expected x86_64, got %q", otherArch())
	}
}

func TestArchMatchesAndTokens(t *testing.T) {
	if !archMatches("file-amd64", "x86_64") {
		t.Error("expected true for file-amd64 and x86_64")
	}
	if archMatches("file-arm64", "x86_64") {
		t.Error("expected false for file-arm64 and x86_64")
	}

	archName = "x86_64"
	if !hasOtherArchToken("file-arm64") {
		t.Error("expected true for file-arm64 when arch is x86_64")
	}
}

func TestOSReleaseField(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	osReleasePath = filepath.Join(tmp, "os-release")

	content := `NAME="Fedora Linux"
VERSION="40 (Workstation Edition)"
ID=fedora
VERSION_ID=40
`
	if err := os.WriteFile(osReleasePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write mock os-release: %v", err)
	}

	if val := osReleaseField("ID"); val != "fedora" {
		t.Errorf("expected fedora, got %q", val)
	}
	if val := osReleaseField("VERSION_ID"); val != "40" {
		t.Errorf("expected 40, got %q", val)
	}
	if val := osReleaseField("NONEXISTENT"); val != "" {
		t.Errorf("expected empty string, got %q", val)
	}

	// Missing file case
	osReleasePath = filepath.Join(tmp, "nonexistent")
	if val := osReleaseField("ID"); val != "" {
		t.Errorf("expected empty string for missing file, got %q", val)
	}
}

func TestDetectRHELAndArchFamilies(t *testing.T) {
	defer resetMocks()

	tmp := t.TempDir()
	osReleasePath = filepath.Join(tmp, "os-release")

	// Test Fedora (RHEL family)
	if err := os.WriteFile(osReleasePath, []byte("ID=fedora\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !detectRHELFamily() {
		t.Error("expected fedora to be detected as RHEL family")
	}
	if detectArchFamily() {
		t.Error("expected fedora to NOT be detected as Arch family")
	}

	// Test Arch (Arch family)
	if err := os.WriteFile(osReleasePath, []byte("ID_LIKE=\"arch\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if detectRHELFamily() {
		t.Error("expected arch to NOT be detected as RHEL family")
	}
	if !detectArchFamily() {
		t.Error("expected arch to be detected as Arch family")
	}

	// Missing file fallback cases
	osReleasePath = filepath.Join(tmp, "nonexistent")
	pkgMgr = "dnf"
	if !detectRHELFamily() {
		t.Error("expected dnf manager fallback to RHEL family")
	}
	pkgMgr = "pacman"
	if !detectArchFamily() {
		t.Error("expected pacman manager fallback to Arch family")
	}
}
