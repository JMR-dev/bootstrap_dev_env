package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const httpClientTimeout = 30 * time.Minute

var httpClient = &http.Client{Timeout: httpClientTimeout}

// downloadReal streams url -> dest. Returns true on success.
func downloadReal(url, dest string) bool {
	taskPrintf("  Downloading %s ...\n", filepath.Base(url))

	dir, file := filepath.Split(dest)
	dir = filepath.Clean(dir)

	// Try aria2c first if available
	if hasCmd("aria2c") {
		res := runCmd([]string{"aria2c", "-x", "16", "-s", "16", "-k", "1M", "-d", dir, "-o", file, url}, CmdOpts{Out: taskOut()})
		if res.OK() {
			return true
		}
		taskPrintf("  [WARN] aria2c download failed for %s, falling back to curl ...\n", url)
	}

	// Fallback to curl
	if hasCmd("curl") {
		res := runCmd([]string{"curl", "-L", "--fail", "-o", dest, url}, CmdOpts{Out: taskOut()})
		if res.OK() {
			return true
		}
		taskPrintf("  [WARN] curl download failed for %s ...\n", url)
	}

	// Final fallback: Go built-in HTTP client
	taskPrintf("  Falling back to built-in HTTP client for %s ...\n", url)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		errLog(fmt.Sprintf("Download failed for %s: %v", url, err))
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		errLog(fmt.Sprintf("Download failed for %s: %v", url, err))
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		errLog(fmt.Sprintf("Download failed for %s: HTTP %d", url, resp.StatusCode))
		return false
	}
	f, err := os.Create(dest)
	if err != nil {
		errLog(fmt.Sprintf("Download failed for %s: %v", url, err))
		return false
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		errLog(fmt.Sprintf("Download failed for %s: %v", url, err))
		return false
	}
	return true
}

// fetchJSONReal GETs url with the GitHub API Accept header and decodes the body
// into v. Returns true on success.
func fetchJSONReal(url string, v any) bool {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		errLog(fmt.Sprintf("API request failed for %s: %v", url, err))
		return false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		errLog(fmt.Sprintf("API request failed for %s: %v", url, err))
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		errLog(fmt.Sprintf("API request failed for %s: HTTP %d", url, resp.StatusCode))
		return false
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		errLog(fmt.Sprintf("API request failed for %s: %v", url, err))
		return false
	}
	return true
}

// fetchTextReal returns the trimmed body of url. Returns empty string on failure.
func fetchTextReal(url string) string {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		errLog(fmt.Sprintf("Fetch failed for %s: %v", url, err))
		return ""
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		errLog(fmt.Sprintf("Fetch failed for %s: %v", url, err))
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		errLog(fmt.Sprintf("Fetch failed for %s: HTTP %d", url, resp.StatusCode))
		return ""
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		errLog(fmt.Sprintf("Fetch failed for %s: %v", url, err))
		return ""
	}
	return strings.TrimSpace(string(b))
}

func sha256Of(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
