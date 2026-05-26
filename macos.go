package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── Xcode + Homebrew prereqs ────────────────────────────────────────────

func brewPrefix() string {
	if archName == "aarch64" {
		return "/opt/homebrew"
	}
	return "/usr/local"
}

func ensureXcodeCLT() {
	if !isMacOS {
		return
	}
	res, ok := probe([]string{"xcode-select", "-p"}, 10*time.Second)
	if ok && res.ExitCode == 0 {
		fmt.Printf("[Xcode CLT] Already installed at %s\n", strings.TrimSpace(string(res.Stdout)))
		return
	}
	fmt.Println("[Xcode CLT] Installing Xcode Command Line Tools ...")
	fmt.Println("           A GUI dialog will appear — click 'Install' to proceed.")
	runCmd([]string{"xcode-select", "--install"}, CmdOpts{Timeout: 30 * time.Second})
	fmt.Println("           Waiting for installation to complete ...")
	for {
		r, ok := probe([]string{"xcode-select", "-p"}, 10*time.Second)
		if ok && r.ExitCode == 0 {
			break
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Println("[Xcode CLT] Installation complete.")
}

func ensureHomebrew() {
	if !isMacOS {
		return
	}
	if hasCmd("brew") {
		path, _ := exec.LookPath("brew")
		fmt.Printf("[Homebrew] Already installed at %s\n", path)
		return
	}
	fmt.Println("[Homebrew] Installing Homebrew ...")
	installer := `NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	if !runShell(installer, CmdOpts{}).OK() {
		fmt.Fprintln(os.Stderr, "Homebrew installation failed")
		osExit(1)
	}

	brewBinDir := filepath.Join(brewPrefix(), "bin")
	brewPath := filepath.Join(brewBinDir, "brew")
	if _, err := osStat(brewPath); err != nil {
		fmt.Fprintf(os.Stderr, "Homebrew installed but brew not found at %s\n", brewPath)
		osExit(1)
	}

	os.Setenv("PATH", brewBinDir+":"+os.Getenv("PATH"))

	shellenvLine := fmt.Sprintf(`eval "$(%s shellenv)"`, brewPath)
	runCmd([]string{
		"bash", "-c",
		fmt.Sprintf("grep -qxF %q /etc/zprofile 2>/dev/null || echo %q >> /etc/zprofile",
			shellenvLine, shellenvLine),
	}, CmdOpts{AsSudo: true})
	fmt.Printf("[Homebrew] Installed at %s; added shellenv to /etc/zprofile\n", brewPrefix())
}

// ── macOS firecracker VM bridge ─────────────────────────────────────────
//
// Firecracker is Linux-only (needs KVM). On macOS we provision a Fedora cloud
// VM via QEMU/HVF, install firecracker inside it, and expose a `firecracker`
// zsh function on the host that proxies invocations over SSH.

const (
	vmSSHPort        = 2222
	vmUser           = "fc"
	vmQcow2Name      = "fedora.qcow2"
	vmSeedISOName    = "seed.iso"
	vmPIDName        = "vm.pid"
	vmKeyName        = "id_ed25519"
	firecrackerFnBeg = "# >>> firecracker-vm wrapper >>>"
	firecrackerFnEnd = "# <<< firecracker-vm wrapper <<<"
)

func vmDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".firecracker-vm")
}

func macosMajor() int {
	if !isMacOS {
		return 0
	}
	r, ok := probe([]string{"sw_vers", "-productVersion"}, 10*time.Second)
	if !ok || r.ExitCode != 0 {
		return 0
	}
	v := strings.TrimSpace(string(r.Stdout))
	if v == "" {
		return 0
	}
	parts := strings.Split(v, ".")
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return n
}

func appleSiliconGeneration() int {
	if !isMacOS || archName != "aarch64" {
		return 0
	}
	r, ok := probe([]string{"sysctl", "-n", "machdep.cpu.brand_string"}, 10*time.Second)
	if !ok || r.ExitCode != 0 {
		return 0
	}
	brand := strings.TrimSpace(string(r.Stdout))
	idx := strings.Index(brand, "Apple M")
	if idx < 0 {
		return 0
	}
	rest := brand[idx+len("Apple M"):]
	var digits []byte
	for i := 0; i < len(rest) && rest[i] >= '0' && rest[i] <= '9'; i++ {
		digits = append(digits, rest[i])
	}
	if len(digits) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(string(digits))
	return n
}

func selectVMBackend() string {
	if !isMacOS {
		return ""
	}
	if archName == "x86_64" {
		fmt.Println("\n[firecracker VM] Intel Mac — using VirtualBox (supports nested VT-x for in-guest KVM).")
		return "virtualbox"
	}
	gen := appleSiliconGeneration()
	macos := macosMajor()
	if gen >= 3 && macos >= 15 {
		fmt.Printf("\n[firecracker VM] Apple Silicon M%d on macOS %d — using QEMU/HVF with nested virtualization (-cpu host,el2=on).\n", gen, macos)
		return "qemu"
	}
	chip := "Apple Silicon"
	if gen != 0 {
		chip = fmt.Sprintf("Apple M%d", gen)
	}
	osStr := "this macOS"
	if macos != 0 {
		osStr = fmt.Sprintf("macOS %d", macos)
	}
	fmt.Println()
	fmt.Println("[firecracker VM] Skipping firecracker VM provisioning.")
	fmt.Printf("  Detected %s on %s. HVF only exposes nested\n", chip, osStr)
	fmt.Println("  virtualization on M3+ chips running macOS 15 Sequoia or later,")
	fmt.Println("  and VirtualBox does not support Apple Silicon hosts, so there")
	fmt.Println("  is no local hypervisor that can run firecracker microVMs here.")
	fmt.Println("  To use firecracker, provision a Linux cloud VM (e.g. AWS EC2,")
	fmt.Println("  GCP) and run firecracker there over SSH.")
	return ""
}

func latestFedoraCloudImage() (filename, qcowURL, checksumURL string, ok bool) {
	base := "https://dl.fedoraproject.org/pub/fedora/linux/releases/"
	listing := fetchText(base)
	if listing == "" {
		return
	}
	verRe := regexp.MustCompile(`href="(\d+)/?"`)
	seen := map[int]bool{}
	var versions []int
	for _, m := range verRe.FindAllStringSubmatch(listing, -1) {
		v, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if !seen[v] {
			seen[v] = true
			versions = append(versions, v)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	for _, ver := range versions {
		imagesURL := fmt.Sprintf("%s%d/Cloud/%s/images/", base, ver, archName)
		idx := fetchText(imagesURL)
		if idx == "" {
			continue
		}
		qcowRe := regexp.MustCompile(fmt.Sprintf(`href="(Fedora-Cloud-Base[A-Za-z0-9_-]*-%d-[\d.]+\.%s\.qcow2)"`, ver, archName))
		ckRe := regexp.MustCompile(`href="([^"]*CHECKSUM)"`)
		qm := qcowRe.FindStringSubmatch(idx)
		cm := ckRe.FindStringSubmatch(idx)
		if qm == nil || cm == nil {
			continue
		}
		return qm[1], imagesURL + qm[1], imagesURL + cm[1], true
	}
	return
}

func verifyFedoraQcow2(qcow2, checksumURL string) bool {
	text := fetchText(checksumURL)
	if text == "" {
		return false
	}
	name := filepath.Base(qcow2)
	re := regexp.MustCompile(fmt.Sprintf(`SHA256 \(%s\) = ([0-9a-fA-F]+)`, regexp.QuoteMeta(name)))
	m := re.FindStringSubmatch(text)
	if m == nil {
		errLog(fmt.Sprintf("No SHA256 entry for %s in checksum file", name))
		return false
	}
	expected := strings.ToLower(m[1])
	fmt.Println("  Verifying SHA256 (this can take a minute) ...")
	actual, err := sha256Of(qcow2)
	if err != nil {
		errLog(fmt.Sprintf("Fedora image hash failed: %v", err))
		return false
	}
	if strings.ToLower(actual) != expected {
		errLog(fmt.Sprintf("Fedora image SHA256 mismatch (got %s, expected %s)", actual, expected))
		return false
	}
	fmt.Println("  SHA256 OK")
	return true
}

func downloadFedoraImage(qcowURL, dest string) bool {
	if !hasCmd("curl") {
		return download(qcowURL, dest)
	}
	fmt.Printf("  Downloading %s ...\n", filepath.Base(dest))
	return runCmd([]string{"curl", "-L", "--fail", "-#", "-o", dest, qcowURL}, CmdOpts{}).OK()
}

const firecrackerUserdataTmpl = `#cloud-config
hostname: firecracker-vm
users:
  - name: %s
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - %s
ssh_pwauth: false
packages:
  - curl
  - tar
  - qemu-kvm
write_files:
  - path: /usr/local/sbin/install-firecracker.sh
    permissions: '0755'
    content: |
      #!/usr/bin/env bash
      set -euo pipefail
      ARCH=$(uname -m)
      TAG=$(curl -fsSL https://api.github.com/repos/firecracker-microvm/firecracker/releases/latest \
            | grep -oE '"tag_name":[[:space:]]*"v[^"]+"' | head -1 \
            | sed -E 's/.*"v([^"]+)"/\1/')
      cd /tmp
      curl -fsSL -o fc.tgz \
        "https://github.com/firecracker-microvm/firecracker/releases/download/v${TAG}/firecracker-v${TAG}-${ARCH}.tgz"
      tar -xzf fc.tgz
      BIN=$(find . -maxdepth 3 -type f -name "firecracker-v${TAG}-${ARCH}" ! -name '*.debug' | head -1)
      install -m 0755 "$BIN" /usr/local/bin/firecracker
      touch /var/lib/firecracker-ready
runcmd:
  - /usr/local/sbin/install-firecracker.sh
`

func writeCloudInitSeed(seedDir, pubkey string) error {
	if err := osMkdirAll(seedDir, 0o755); err != nil {
		return err
	}
	userData := fmt.Sprintf(firecrackerUserdataTmpl, vmUser, strings.TrimSpace(pubkey))
	if err := osWriteFile(filepath.Join(seedDir, "user-data"), []byte(userData), 0o644); err != nil {
		return err
	}
	return osWriteFile(filepath.Join(seedDir, "meta-data"),
		[]byte("instance-id: firecracker-vm\nlocal-hostname: firecracker-vm\n"), 0o644)
}

func buildSeedISO(seedDir, isoPath string) bool {
	osRemove(isoPath)
	return runCmd([]string{
		"hdiutil", "makehybrid", "-iso", "-joliet",
		"-default-volume-name", "cidata",
		"-o", isoPath, seedDir,
	}, CmdOpts{}).OK()
}

func writeQEMUStartScript() string {
	dir := vmDir()
	brewShare := filepath.Join(brewPrefix(), "share", "qemu")
	scriptPath := filepath.Join(dir, "vm-start.sh")
	edkCode := filepath.Join(brewShare, "edk2-aarch64-code.fd")
	edkVarsTemplate := filepath.Join(brewShare, "edk2-arm-vars.fd")

	qemuBlock := fmt.Sprintf(`# Ensure a writable NVRAM file exists (UEFI vars persist here).
if [[ ! -f edk2-aarch64-vars.fd ]]; then
    if [[ -f "%s" ]]; then
        cp "%s" edk2-aarch64-vars.fd
    else
        truncate -s 64M edk2-aarch64-vars.fd
    fi
fi

exec qemu-system-aarch64 \
    -machine virt,accel=hvf,highmem=on \
    -cpu host,el2=on \
    -smp 2 -m 2048 \
    -drive if=pflash,format=raw,readonly=on,file="%s" \
    -drive if=pflash,format=raw,file=edk2-aarch64-vars.fd \
    -drive file=%s,if=virtio,format=qcow2 \
    -drive file=%s,format=raw,if=virtio,readonly=on \
    -display none -serial file:vm.log \
    -netdev user,id=net0,hostfwd=tcp::%d-:22 \
    -device virtio-net-device,netdev=net0 \
    -daemonize -pidfile %s
`, edkVarsTemplate, edkVarsTemplate, edkCode, vmQcow2Name, vmSeedISOName, vmSSHPort, vmPIDName)

	script := fmt.Sprintf(`#!/usr/bin/env bash
# Start the Fedora-on-QEMU VM that backs the host firecracker zsh function.
set -euo pipefail
cd "%s"
if [[ -f %s ]] && kill -0 "$(cat %s)" 2>/dev/null; then
    exit 0
fi
rm -f %s
%s`, dir, vmPIDName, vmPIDName, vmPIDName, qemuBlock)

	osWriteFile(scriptPath, []byte(script), 0o755)
	return scriptPath
}

func sshToVM(privKey string, remote []string, timeout time.Duration) CmdResult {
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	args := []string{
		"-q",
		"-i", privKey,
		"-p", strconv.Itoa(vmSSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(timeout.Seconds())),
		"-o", "LogLevel=ERROR",
		fmt.Sprintf("%s@127.0.0.1", vmUser),
	}
	args = append(args, remote...)
	r, ok := probe(append([]string{"ssh"}, args...), timeout+30*time.Second)
	if !ok {
		return CmdResult{ExitCode: 124}
	}
	return r
}

func waitForVMSSH(privKey string, timeout time.Duration) bool {
	fmt.Printf("  Waiting for VM SSH on port %d (up to %s) ...\n", vmSSHPort, timeout)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sshToVM(privKey, []string{"true"}, 0).OK() {
			fmt.Println("  VM SSH ready.")
			return true
		}
		time.Sleep(5 * time.Second)
	}
	return false
}

func waitForFirecrackerInVM(privKey string, timeout time.Duration) bool {
	fmt.Printf("  Waiting for cloud-init to install firecracker inside the VM (up to %s) ...\n", timeout)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sshToVM(privKey, []string{"test", "-f", "/var/lib/firecracker-ready"}, 0).OK() {
			fmt.Println("  firecracker is installed inside the VM.")
			return true
		}
		time.Sleep(10 * time.Second)
	}
	return false
}

func firecrackerZshFunction(privKey string) string {
	return fmt.Sprintf(`%s
firecracker() {
    local vm_dir="%s"
    if [[ ! -f "$vm_dir/%s" ]]; then
        echo "firecracker: Fedora VM not provisioned (expected $vm_dir/%s)." >&2
        return 1
    fi
    if ! "$vm_dir/vm-start.sh"; then
        echo "firecracker: failed to start backing VM (see $vm_dir/vm.log)." >&2
        return 1
    fi
    local i
    for i in $(seq 1 60); do
        ssh -q -i "%s" -p %d \
            -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
            -o ConnectTimeout=2 -o LogLevel=ERROR \
            %s@127.0.0.1 true && break
        sleep 1
    done
    local args=() a
    for a in "$@"; do args+=("$(printf %%q "$a")"); done
    ssh -t -q -i "%s" -p %d \
        -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR \
        %s@127.0.0.1 "sudo /usr/local/bin/firecracker ${args[*]}"
}
%s
`,
		firecrackerFnBeg,
		vmDir(),
		vmQcow2Name, vmQcow2Name,
		privKey, vmSSHPort, vmUser,
		privKey, vmSSHPort, vmUser,
		firecrackerFnEnd,
	)
}

func installFirecrackerZshFunction(content string) {
	home, _ := os.UserHomeDir()
	zshrc := filepath.Join(home, ".zshrc")
	existing := ""
	if b, err := osReadFile(zshrc); err == nil {
		existing = string(b)
	}
	pattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(firecrackerFnBeg) + `.*?` + regexp.QuoteMeta(firecrackerFnEnd) + `\n?`)
	var newContent string
	if pattern.MatchString(existing) {
		newContent = pattern.ReplaceAllString(existing, content)
	} else {
		if existing != "" {
			newContent = strings.TrimRight(existing, "\n") + "\n\n" + content
		} else {
			newContent = content
		}
	}
	osWriteFile(zshrc, []byte(newContent), 0o644)
	fmt.Printf("  Wrote firecracker() function block to %s\n", zshrc)
}

func ensureVirtualBox() bool {
	if hasCmd("VBoxManage") {
		return true
	}
	fmt.Println("  Installing VirtualBox via brew cask ...")
	if !runCmd([]string{"brew", "install", "--cask", "virtualbox"}, CmdOpts{}).OK() {
		errLog("VirtualBox cask install failed. macOS may require kernel-extension " +
			"approval in System Settings → Privacy & Security; once approved, re-run this script.")
		return false
	}
	if !hasCmd("VBoxManage") {
		errLog("VirtualBox installed but VBoxManage not in PATH. macOS may need a reboot or kext approval.")
		return false
	}
	return true
}

func provisionVirtualBoxVM(qcow2, seedISO string) string {
	if !ensureVirtualBox() {
		return ""
	}
	vmName := "firecracker-vm"
	dir := vmDir()
	vboxBase := filepath.Join(dir, "vbox")
	vdi := filepath.Join(dir, "fedora.vdi")

	r, ok := probe([]string{"VBoxManage", "showvminfo", vmName}, 30*time.Second)
	exists := ok && r.ExitCode == 0

	if !exists {
		if _, err := osStat(vdi); os.IsNotExist(err) {
			fmt.Printf("  Converting %s → %s (VirtualBox VDI) ...\n", filepath.Base(qcow2), filepath.Base(vdi))
			if !runCmd([]string{"VBoxManage", "clonemedium", "disk", qcow2, vdi, "--format", "VDI"}, CmdOpts{}).OK() {
				errLog("VBoxManage clonemedium failed")
				return ""
			}
			runCmd([]string{"VBoxManage", "modifymedium", "disk", vdi, "--resize", "10240"}, CmdOpts{})
		}
		fmt.Printf("  Creating VirtualBox VM '%s' ...\n", vmName)
		osMkdirAll(vboxBase, 0o755)
		if !runCmd([]string{
			"VBoxManage", "createvm",
			"--name", vmName,
			"--ostype", "Fedora_64",
			"--basefolder", vboxBase,
			"--register",
		}, CmdOpts{}).OK() {
			errLog("VBoxManage createvm failed")
			return ""
		}
		runCmd([]string{
			"VBoxManage", "modifyvm", vmName,
			"--cpus", "2",
			"--memory", "2048",
			"--nested-hw-virt", "on",
			"--nic1", "nat",
			"--natpf1", fmt.Sprintf("ssh,tcp,,%d,,22", vmSSHPort),
		}, CmdOpts{})
		runCmd([]string{"VBoxManage", "storagectl", vmName, "--name", "SATA", "--add", "sata"}, CmdOpts{})
		runCmd([]string{
			"VBoxManage", "storageattach", vmName,
			"--storagectl", "SATA",
			"--port", "0", "--device", "0", "--type", "hdd",
			"--medium", vdi,
		}, CmdOpts{})
		runCmd([]string{"VBoxManage", "storagectl", vmName, "--name", "IDE", "--add", "ide"}, CmdOpts{})
		runCmd([]string{
			"VBoxManage", "storageattach", vmName,
			"--storagectl", "IDE",
			"--port", "0", "--device", "0", "--type", "dvddrive",
			"--medium", seedISO,
		}, CmdOpts{})
	} else {
		fmt.Printf("  VirtualBox VM '%s' already registered — reusing.\n", vmName)
	}

	scriptPath := filepath.Join(dir, "vm-start.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
# Start the VirtualBox-backed Fedora VM that powers the host firecracker() fn.
set -euo pipefail
if VBoxManage list runningvms | grep -q '"%s"'; then
    exit 0
fi
exec VBoxManage startvm %s --type headless
`, vmName, vmName)
	osWriteFile(scriptPath, []byte(script), 0o755)
	return scriptPath
}

func setupFirecrackerVM() {
	if !isMacOS {
		return
	}
	backend := selectVMBackend()
	if backend == "" {
		return
	}

	fmt.Println("\n=== macOS firecracker VM (Fedora) ===")
	dir := vmDir()
	osMkdirAll(dir, 0o755)

	privKey := filepath.Join(dir, vmKeyName)
	pubKey := privKey + ".pub"
	if _, err := osStat(privKey); os.IsNotExist(err) {
		fmt.Printf("  Generating SSH keypair at %s ...\n", privKey)
		if !runCmd([]string{"ssh-keygen", "-t", "ed25519", "-N", "", "-f", privKey, "-q"}, CmdOpts{}).OK() {
			errLog("ssh-keygen failed — aborting VM setup")
			return
		}
	}

	qcow2 := filepath.Join(dir, vmQcow2Name)
	if _, err := osStat(qcow2); err == nil {
		fmt.Printf("  Reusing existing Fedora image at %s\n", qcow2)
	} else {
		fmt.Println("  Looking up latest Fedora cloud image ...")
		filename, qcowURL, checksumURL, ok := latestFedoraCloudImage()
		if !ok {
			errLog("Could not resolve latest Fedora cloud image — aborting VM setup")
			return
		}
		fmt.Printf("  Latest: %s\n", filename)
		downloadDest := filepath.Join(dir, filename)
		if !downloadFedoraImage(qcowURL, downloadDest) {
			errLog("Fedora image download failed — aborting VM setup")
			return
		}
		if !verifyFedoraQcow2(downloadDest, checksumURL) {
			osRemove(downloadDest)
			return
		}
		osRename(downloadDest, qcow2)
		if hasCmd("qemu-img") {
			fmt.Println("  Resizing image to 10G ...")
			runCmd([]string{"qemu-img", "resize", qcow2, "10G"}, CmdOpts{})
		}
	}

	fmt.Println("  Building cloud-init seed ISO ...")
	seedDir := filepath.Join(dir, "seed")
	pubKeyBytes, err := osReadFile(pubKey)
	if err != nil {
		errLog(fmt.Sprintf("could not read public key: %v", err))
		return
	}
	if err := writeCloudInitSeed(seedDir, string(pubKeyBytes)); err != nil {
		errLog(fmt.Sprintf("cloud-init seed write failed: %v", err))
		return
	}
	seedISO := filepath.Join(dir, vmSeedISOName)
	if !buildSeedISO(seedDir, seedISO) {
		errLog("hdiutil failed to build seed ISO — aborting VM setup")
		return
	}

	var startScript string
	if backend == "qemu" {
		if !hasCmd("qemu-system-aarch64") {
			errLog("qemu-system-aarch64 not found — install qemu via brew first.")
			return
		}
		fmt.Println("  Writing QEMU start script ...")
		startScript = writeQEMUStartScript()
	} else {
		startScript = provisionVirtualBoxVM(qcow2, seedISO)
		if startScript == "" {
			return
		}
	}

	fmt.Printf("  Booting VM via %s ...\n", startScript)
	if !runCmd([]string{startScript}, CmdOpts{}).OK() {
		errLog(fmt.Sprintf("VM start failed — see %s", filepath.Join(dir, "vm.log")))
		return
	}

	if !waitForVMSSH(privKey, 5*time.Minute) {
		errLog(fmt.Sprintf("VM SSH never came up — see %s", filepath.Join(dir, "vm.log")))
		return
	}

	if !waitForFirecrackerInVM(privKey, 15*time.Minute) {
		warn("firecracker did not appear in the VM within the timeout; " +
			"cloud-init may still be running. Check `sudo cloud-init status` " +
			"inside the VM (ssh -i ~/.firecracker-vm/id_ed25519 -p 2222 fc@127.0.0.1).")
	}

	fmt.Println("  Installing firecracker() wrapper into ~/.zshrc ...")
	installFirecrackerZshFunction(firecrackerZshFunction(privKey))

	fmt.Printf("  firecracker VM ready (backend: %s).\n", backend)
	fmt.Printf("  Start manually with: %s\n", startScript)
	if backend == "qemu" {
		fmt.Println("  Nested virt is on (el2=on); the guest's KVM can launch firecracker microVMs.")
	} else {
		fmt.Println("  Nested VT-x is on; the guest's KVM can launch firecracker microVMs.")
	}
}
