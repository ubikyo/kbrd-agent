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
	"sort"
	"strings"
	"sync"
)

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

	// Whichever of them the Mac itself opens a link with, so the editors
	// can offer it rather than asking for an answer the system already
	// has. Safari when LaunchServices records no choice of its own — see
	// `defaultBrowserID`.
	preferred := readDefaultBrowserID(ctx)
	if preferred == "" {
		preferred = SafariBundleID
	}

	browsers := make([]Browser, 0, len(byID))
	for _, browser := range byID {
		browser.Default = strings.EqualFold(browser.ID, preferred)
		browsers = append(browsers, browser)
	}
	sort.Slice(browsers, func(left, right int) bool {
		return strings.ToLower(browsers[left].Name) < strings.ToLower(browsers[right].Name)
	})
	return browsers, nil
}

// readDefaultBrowserID converts LaunchServices' own preferences with the
// same `plutil` the bundles are read through, and hands them to
// `defaultBrowserID`. Every failure here is the same answer as "no
// choice recorded": the file is absent on a Mac still using Safari,
// which is not a state worth failing the whole list over.
func readDefaultBrowserID(ctx context.Context) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	output, err := exec.CommandContext(
		ctx,
		"/usr/bin/plutil",
		"-convert",
		"json",
		"-o",
		"-",
		filepath.Join(
			home,
			"Library/Preferences/com.apple.LaunchServices",
			"com.apple.launchservices.secure.plist",
		),
	).Output()
	if err != nil {
		return ""
	}
	return defaultBrowserID(output)
}

// readBrowser accepts an .app bundle only if its Info.plist says it is a
// web browser rather than merely an opener of http links — see
// `isWebBrowser` for the difference, and for what was turning up in the
// list while the two were treated as the same thing.
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
	if !isWebBrowser(info, bundleID) {
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
