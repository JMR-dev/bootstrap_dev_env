package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"dagger.io/dagger"
	"golang.org/x/sync/errgroup"
)

func main() {
	osFlag := flag.String("os", "all", "OS to test (debian, arch, fedora, or all)")
	flag.Parse()

	ctx := context.Background()

	// Initialize Dagger Client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Dagger: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Get reference to the project source directory
	src := client.Host().Directory(".")

	// Build the bootstrapping binary inside Go container
	fmt.Println("Building bootstrap_environment binary for Linux...")
	builder := client.Container().
		From("golang:1.25").
		WithMountedDirectory("/src", src).
		WithWorkdir("/src").
		WithExec([]string{"go", "build", "-buildvcs=false", "-o", "bootstrap_environment", "."})

	binaryFile := builder.File("bootstrap_environment")

	// Target OS list
	var targets []string
	switch strings.ToLower(*osFlag) {
	case "debian":
		targets = []string{"debian:latest"}
	case "arch":
		targets = []string{"archlinux:latest"}
	case "fedora":
		targets = []string{"fedora:latest"}
	case "all":
		targets = []string{"debian:latest", "archlinux:latest", "fedora:latest"}
	default:
		fmt.Fprintf(os.Stderr, "Unsupported OS: %s. Supported: debian, arch, fedora, all\n", *osFlag)
		os.Exit(1)
	}

	g, ctx := errgroup.WithContext(ctx)

	for _, target := range targets {
		target := target // capture loop variable
		g.Go(func() error {
			fmt.Printf("=== Starting integration test on target OS: %s ===\n", target)

			// 1. Prepare target container base and setup script based on OS distro
			var testContainer *dagger.Container
			if strings.Contains(target, "debian") {
				testContainer = client.Container().
					From(target).
					WithExec([]string{"apt-get", "update"}).
					WithExec([]string{"apt-get", "install", "-y", "sudo", "curl", "git", "wget", "tar", "unzip", "xz-utils", "make", "python3", "which"})
			} else if strings.Contains(target, "fedora") {
				testContainer = client.Container().
					From(target).
					WithExec([]string{"dnf", "install", "-y", "sudo", "curl", "git", "wget", "tar", "unzip", "xz", "make", "python3", "which"})
			} else if strings.Contains(target, "archlinux") {
				testContainer = client.Container().
					From(target).
					WithExec([]string{"pacman", "-Sy", "--noconfirm", "sudo", "curl", "git", "wget", "tar", "unzip", "xz", "make", "python", "which"})
			} else {
				return fmt.Errorf("unsupported target OS: %s", target)
			}

			// 2. Pre-create the ~/.pyenv directory to skip python source compilation in integration test (saves ~10 minutes)
			testContainer = testContainer.WithExec([]string{"mkdir", "-p", "/root/.pyenv"})

			// 3. Mount the built binary
			testContainer = testContainer.
				WithFile("/usr/local/bin/bootstrap_environment", binaryFile).
				WithWorkdir("/tmp")

			// Forward GitHub token so API calls are authenticated (avoids 403 rate-limits)
			if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
				secret := client.SetSecret("github-token", tok)
				testContainer = testContainer.
					WithSecretVariable("GITHUB_TOKEN", secret).
					WithSecretVariable("GH_TOKEN", secret)
			}

			// 4. Run bootstrap binary against the full package set (no scope flags).
			// We pipe 'y' to satisfy the "Proceed? [y/N]" prompt.
			fmt.Printf("[%s] Executing bootstrap_environment...\n", target)
			testContainer = testContainer.WithExec([]string{"sh", "-c", "echo y | bootstrap_environment --gui"})

			// 5. Verify all installed custom packages return a path and zero exit code from version command
			fmt.Printf("[%s] Verifying package installations on PATH and running version checks...\n", target)
			verifyCmd := []string{
				"sh", "-c",
				"set -e -x; " +
					"export PATH=$PATH:/usr/local/go/bin:/usr/local/bin; " +
					"which go && go version && " +
					"which nvim && nvim --version && " +
					"which zig && zig version && " +
					"which firecracker && firecracker --version",
			}
			verifyOutput, err := testContainer.WithExec(verifyCmd).Stdout(ctx)
			if err != nil {
				// To see the stdout/stderr of the failing command, we can try to extract it from dagger's ExecError
				return fmt.Errorf("verification failed on %s: %v", target, err)
			}


			fmt.Printf("[%s] Verification Output:\n%s\n", target, verifyOutput)
			fmt.Printf("--- PASS: Integration test on %s completed successfully ---\n", target)
			return nil

		})
	}

	if err := g.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "One or more integration tests failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nAll parallel integration tests passed successfully!")
}
