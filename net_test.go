package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type mockTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestDownloadReal(t *testing.T) {
	defer resetMocks()

	// Mock HTTP client
	oldTransport := httpClient.Transport
	defer func() { httpClient.Transport = oldTransport }()

	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == "https://example.com/file" {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString("hello download")),
				}, nil
			}
			return &http.Response{
				StatusCode: 404,
				Body:       io.NopCloser(bytes.NewBufferString("not found")),
			}, nil
		},
	}

	tmpDir := t.TempDir()
	destFile := filepath.Join(tmpDir, "out.txt")

	// Successful download
	success := downloadReal("https://example.com/file", destFile)
	if !success {
		t.Fatal("expected download to succeed")
	}

	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != "hello download" {
		t.Errorf("expected 'hello download', got %q", string(data))
	}

	// Failed download (404)
	failDest := filepath.Join(tmpDir, "out_fail.txt")
	successFail := downloadReal("https://example.com/nonexistent", failDest)
	if successFail {
		t.Error("expected download to fail with 404")
	}
}

func TestFetchJSONReal(t *testing.T) {
	defer resetMocks()

	oldTransport := httpClient.Transport
	defer func() { httpClient.Transport = oldTransport }()

	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == "https://example.com/api" {
				// Verify auth header if token is set
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"key": "value"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(bytes.NewBufferString("internal error")),
			}, nil
		},
	}

	type MockResponse struct {
		Key string `json:"key"`
	}

	var res MockResponse
	success := fetchJSONReal("https://example.com/api", &res)
	if !success {
		t.Fatal("expected fetchJSON to succeed")
	}
	if res.Key != "value" {
		t.Errorf("expected Key to be 'value', got %q", res.Key)
	}

	successFail := fetchJSONReal("https://example.com/bad", &res)
	if successFail {
		t.Error("expected fetchJSON to fail with 500")
	}
}

func TestFetchTextReal(t *testing.T) {
	defer resetMocks()

	oldTransport := httpClient.Transport
	defer func() { httpClient.Transport = oldTransport }()

	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == "https://example.com/text" {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString("   Adoptium Latest   \n")),
				}, nil
			}
			return &http.Response{
				StatusCode: 403,
				Body:       io.NopCloser(bytes.NewBufferString("forbidden")),
			}, nil
		},
	}

	text := fetchTextReal("https://example.com/text")
	if text != "Adoptium Latest" {
		t.Errorf("expected trimmed text 'Adoptium Latest', got %q", text)
	}

	textFail := fetchTextReal("https://example.com/forbidden")
	if textFail != "" {
		t.Errorf("expected empty string for failed request, got %q", textFail)
	}
}

func TestSha256Of(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "hash.txt")
	if err := os.WriteFile(path, []byte("hello sha256"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hex of sha256("hello sha256") is 433855b7d2b96c23a6f60e70c655eb4305e8806b682a9596a200642f947259b1
	expected := "433855b7d2b96c23a6f60e70c655eb4305e8806b682a9596a200642f947259b1"
	actual, err := sha256Of(path)
	if err != nil {
		t.Fatalf("failed to calculate hash: %v", err)
	}
	if actual != expected {
		t.Errorf("expected %s, got %s", expected, actual)
	}

	// Nonexistent file
	_, errNonexistent := sha256Of(filepath.Join(tmpDir, "nonexistent"))
	if errNonexistent == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestNetRealErrors(t *testing.T) {
	defer resetMocks()

	oldTransport := httpClient.Transport
	defer func() { httpClient.Transport = oldTransport }()

	// 1. NewRequest error
	if downloadReal("%%%", "dest") {
		t.Error("expected downloadReal to fail for invalid URL")
	}
	if fetchJSONReal("%%%", nil) {
		t.Error("expected fetchJSONReal to fail for invalid URL")
	}
	if fetchTextReal("%%%") != "" {
		t.Error("expected fetchTextReal to fail for invalid URL")
	}

	// 2. Transport Do error
	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	if downloadReal("https://example.com/file", "dest") {
		t.Error("expected downloadReal to fail on connection error")
	}
	if fetchJSONReal("https://example.com/api", nil) {
		t.Error("expected fetchJSONReal to fail on connection error")
	}
	if fetchTextReal("https://example.com/text") != "" {
		t.Error("expected fetchTextReal to fail on connection error")
	}

	// 3. os.Create error
	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString("ok")),
			}, nil
		},
	}
	if downloadReal("https://example.com/file", "/nonexistent-dir/dest") {
		t.Error("expected downloadReal to fail when creating destination file fails")
	}

	// 4. json Decode error
	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString("invalid json")),
			}, nil
		},
	}
	var v any
	if fetchJSONReal("https://example.com/api", &v) {
		t.Error("expected fetchJSONReal to fail on invalid JSON")
	}

	// 5. io.ReadAll error
	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(&errReader{}),
			}, nil
		},
	}
	if fetchTextReal("https://example.com/text") != "" {
		t.Error("expected fetchTextReal to fail on read error")
	}
}

func TestDownloadRealFallbackChain(t *testing.T) {
	defer resetMocks()

	tmpDir := t.TempDir()
	destFile := filepath.Join(tmpDir, "out.txt")

	// Case 1: aria2c works
	var aria2cCalled bool
	var curlCalled bool
	hasCmd = func(name string) bool {
		if name == "aria2c" || name == "curl" {
			return true
		}
		return false
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "aria2c" {
			aria2cCalled = true
			_ = os.WriteFile(destFile, []byte("aria2c content"), 0644)
			return CmdResult{ExitCode: 0}
		}
		if argv[0] == "curl" {
			curlCalled = true
			return CmdResult{ExitCode: 0}
		}
		return CmdResult{ExitCode: 1}
	}

	success := downloadReal("https://example.com/file", destFile)
	if !success {
		t.Fatal("expected download via aria2c to succeed")
	}
	if !aria2cCalled {
		t.Error("expected aria2c to be called")
	}
	if curlCalled {
		t.Error("expected curl NOT to be called when aria2c succeeds")
	}

	// Case 2: aria2c fails, falls back to curl, curl succeeds
	resetMocks()
	aria2cCalled = false
	curlCalled = false
	hasCmd = func(name string) bool {
		if name == "aria2c" || name == "curl" {
			return true
		}
		return false
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "aria2c" {
			aria2cCalled = true
			return CmdResult{ExitCode: 1, Err: fmt.Errorf("aria2c simulated error")}
		}
		if argv[0] == "curl" {
			curlCalled = true
			_ = os.WriteFile(destFile, []byte("curl content"), 0644)
			return CmdResult{ExitCode: 0}
		}
		return CmdResult{ExitCode: 1}
	}

	success = downloadReal("https://example.com/file", destFile)
	if !success {
		t.Fatal("expected download to succeed via curl fallback")
	}
	if !aria2cCalled {
		t.Error("expected aria2c to be attempted")
	}
	if !curlCalled {
		t.Error("expected curl to be attempted after aria2c failed")
	}

	// Case 3: aria2c fails, curl fails, falls back to Go HTTP client
	resetMocks()
	aria2cCalled = false
	curlCalled = false
	hasCmd = func(name string) bool {
		if name == "aria2c" || name == "curl" {
			return true
		}
		return false
	}
	runCmd = func(argv []string, opts CmdOpts) CmdResult {
		if argv[0] == "aria2c" {
			aria2cCalled = true
			return CmdResult{ExitCode: 1, Err: fmt.Errorf("aria2c simulated error")}
		}
		if argv[0] == "curl" {
			curlCalled = true
			return CmdResult{ExitCode: 1, Err: fmt.Errorf("curl simulated error")}
		}
		return CmdResult{ExitCode: 1}
	}
	oldTransport := httpClient.Transport
	defer func() { httpClient.Transport = oldTransport }()
	httpClient.Transport = &mockTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString("go http content")),
			}, nil
		},
	}

	success = downloadReal("https://example.com/file", destFile)
	if !success {
		t.Fatal("expected download to succeed via Go http fallback")
	}
	if !aria2cCalled {
		t.Error("expected aria2c to be attempted")
	}
	if !curlCalled {
		t.Error("expected curl to be attempted")
	}
	data, _ := os.ReadFile(destFile)
	if string(data) != "go http content" {
		t.Errorf("expected file content to be 'go http content', got %q", string(data))
	}
}

