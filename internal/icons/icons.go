// Package icons downloads and locally caches image URLs submitted for
// special-date icons via the configuration UI, so the running application
// never has to hot-link third-party images at render time.
package icons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// httpClient is overridable in tests.
var httpClient = &http.Client{Timeout: 20 * time.Second}

// maxIconBytes caps how large a downloaded icon may be, as a basic
// safety/abuse limit.
const maxIconBytes = 5 << 20 // 5 MiB

// CacheDir is where downloaded icons are saved, relative to the process's
// current working directory -- the same convention used for static/template
// overrides (see webapp.overlayFS), so files saved here are immediately
// servable at "/static/special-icons/<file>" without any extra wiring.
const CacheDir = "static/special-icons"

// IsRemote reports whether an icon field value is a URL that should be
// downloaded and cached locally, rather than rendered as-is (plain
// text/emoji, or an already-local path from a previous download).
func IsRemote(icon string) bool {
	lower := strings.ToLower(icon)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

var extByContentType = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/gif":     ".gif",
	"image/svg+xml": ".svg",
	"image/webp":    ".webp",
}

var imageExtensions = []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp"}

// Fetch downloads iconURL, validates that the response looks like a
// supported image, and saves it under CacheDir using a filename derived
// from the URL (so saving the same URL again reuses the cached file
// without hitting the network). It returns the local path (with a leading
// "/", e.g. "/static/special-icons/<hash>.png") to store in the
// configuration instead of the original URL.
func Fetch(ctx context.Context, iconURL string) (string, error) {
	u, err := url.Parse(iconURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid icon URL %q", iconURL)
	}

	sum := sha256.Sum256([]byte(iconURL))
	name := hex.EncodeToString(sum[:])[:16]
	if cached, ok := findCached(name); ok {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "calendar/1.0 (+special date icon fetcher)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", iconURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: unexpected status %s", iconURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", iconURL, err)
	}
	if len(body) > maxIconBytes {
		return "", fmt.Errorf("icon at %s exceeds the %d byte limit", iconURL, maxIconBytes)
	}

	ext := extensionFor(resp.Header.Get("Content-Type"), iconURL, body)
	if ext == "" {
		return "", fmt.Errorf("%s does not look like a supported image (png/jpeg/gif/svg/webp)", iconURL)
	}

	if err := os.MkdirAll(CacheDir, 0o755); err != nil {
		return "", fmt.Errorf("creating icon cache dir: %w", err)
	}
	localPath := path.Join(CacheDir, name+ext)
	if err := os.WriteFile(localPath, body, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", localPath, err)
	}
	return "/" + localPath, nil
}

// findCached looks for an already-downloaded file for the given URL hash,
// regardless of its extension.
func findCached(name string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(CacheDir, name+".*"))
	if len(matches) == 0 {
		return "", false
	}
	return "/" + filepath.ToSlash(matches[0]), true
}

// extensionFor determines a file extension for downloaded icon bytes,
// preferring the response's Content-Type, then the URL's own extension,
// then sniffing the content itself.
func extensionFor(contentType, iconURL string, body []byte) string {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if ext, ok := extByContentType[ct]; ok {
		return ext
	}
	lowerURL := strings.ToLower(strings.SplitN(iconURL, "?", 2)[0])
	for _, ext := range imageExtensions {
		if strings.HasSuffix(lowerURL, ext) {
			if ext == ".jpeg" {
				ext = ".jpg"
			}
			return ext
		}
	}
	switch http.DetectContentType(body) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	}
	if strings.HasPrefix(http.DetectContentType(body), "image/jpeg") {
		return ".jpg"
	}
	return ""
}
