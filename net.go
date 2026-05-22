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

// download streams url -> dest. Returns true on success.
func download(url, dest string) bool {
	fmt.Printf("  Downloading %s ...\n", filepath.Base(url))
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

// fetchJSON GETs url with the GitHub API Accept header and decodes the body
// into v. Returns true on success.
func fetchJSON(url string, v any) bool {
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

// fetchText returns the trimmed body of url. Returns empty string on failure.
func fetchText(url string) string {
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
