//go:build darwin

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var validBundleID = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

type macOSService struct {
	mu       sync.RWMutex
	browsers map[string]Browser
}

func NewService() Service {
	return &macOSService{browsers: make(map[string]Browser)}
}

func (service *macOSService) List(ctx context.Context) ([]Browser, error) {
	browsers, err := discoverBrowsers(ctx)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	service.browsers = make(map[string]Browser, len(browsers))
	for _, browser := range browsers {
		service.browsers[browser.ID] = browser
	}
	service.mu.Unlock()
	return browsers, nil
}

func (service *macOSService) Open(ctx context.Context, id string, rawURL string) error {
	target, err := parseHTTPURL(rawURL)
	if err != nil {
		return err
	}
	browser, err := service.find(ctx, id)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "/usr/bin/open", "-a", browser.Path, target).Run()
}

func (service *macOSService) find(ctx context.Context, id string) (Browser, error) {
	service.mu.RLock()
	browser, found := service.browsers[id]
	service.mu.RUnlock()
	if found {
		return browser, nil
	}
	if _, err := service.List(ctx); err != nil {
		return Browser{}, err
	}
	service.mu.RLock()
	browser, found = service.browsers[id]
	service.mu.RUnlock()
	if !found {
		return Browser{}, errors.New("browser not found")
	}
	return browser, nil
}

// parseHTTPURL rejects anything that is not a well-formed absolute http(s)
// URL, so a malformed or malicious config value can never reach `open` as
// something that looks like a flag or a non-http scheme (e.g. `file://`).
func parseHTTPURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid http(s) url")
	}
	return parsed.String(), nil
}

func discoverBrowsers(ctx context.Context) ([]Browser, error) {
	home, _ := os.UserHomeDir()
	roots := []string{"/Applications", "/System/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}

	byID := make(map[string]Browser)
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || !entry.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".app") {
				return nil
			}
			if browser, err := readBrowser(ctx, path); err == nil {
				if _, exists := byID[browser.ID]; !exists {
					byID[browser.ID] = browser
				}
			}
			return filepath.SkipDir
		})
	}

	browsers := make([]Browser, 0, len(byID))
	for _, browser := range byID {
		browsers = append(browsers, browser)
	}
	sort.Slice(browsers, func(left, right int) bool {
		return strings.ToLower(browsers[left].Name) < strings.ToLower(browsers[right].Name)
	})
	return browsers, nil
}

// readBrowser accepts an .app bundle only if it declares handling the
// `http`/`https` URL schemes in its Info.plist — the same mechanism macOS
// itself uses to know which apps are eligible to be the default browser.
func readBrowser(ctx context.Context, path string) (Browser, error) {
	plistPath := filepath.Join(path, "Contents", "Info.plist")
	output, err := exec.CommandContext(
		ctx,
		"/usr/bin/plutil",
		"-convert",
		"json",
		"-o",
		"-",
		plistPath,
	).Output()
	if err != nil {
		return Browser{}, err
	}
	var info map[string]any
	if err := json.Unmarshal(output, &info); err != nil {
		return Browser{}, err
	}
	bundleID, _ := info["CFBundleIdentifier"].(string)
	if !validBundleID.MatchString(bundleID) {
		return Browser{}, errors.New("missing or invalid bundle identifier")
	}
	if !handlesHTTP(info["CFBundleURLTypes"]) {
		return Browser{}, errors.New("not a web browser")
	}
	name := firstString(
		info["CFBundleDisplayName"],
		info["CFBundleName"],
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	)
	if name == "" {
		return Browser{}, errors.New("missing application name")
	}
	return Browser{ID: bundleID, Name: name, Path: path}, nil
}

func handlesHTTP(urlTypes any) bool {
	types, ok := urlTypes.([]any)
	if !ok {
		return false
	}
	for _, entry := range types {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		schemes, ok := fields["CFBundleURLSchemes"].([]any)
		if !ok {
			continue
		}
		for _, scheme := range schemes {
			text, ok := scheme.(string)
			if ok && (strings.EqualFold(text, "http") || strings.EqualFold(text, "https")) {
				return true
			}
		}
	}
	return false
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
