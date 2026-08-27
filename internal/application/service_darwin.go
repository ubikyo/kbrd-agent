//go:build darwin

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	mu   sync.RWMutex
	apps map[string]Application
}

func NewService() Service {
	return &macOSService{apps: make(map[string]Application)}
}

func (service *macOSService) List(ctx context.Context) ([]Application, error) {
	apps, err := discoverApplications(ctx)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	service.apps = make(map[string]Application, len(apps))
	for _, app := range apps {
		service.apps[app.ID] = app
	}
	service.mu.Unlock()
	return apps, nil
}

func (service *macOSService) Launch(ctx context.Context, id string) error {
	app, err := service.find(ctx, id)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "/usr/bin/open", app.Path).Run()
}

func (service *macOSService) Quit(ctx context.Context, id string) error {
	app, err := service.find(ctx, id)
	if err != nil {
		return err
	}
	if !app.CanQuit {
		return errors.New("application cannot be quit by bundle identifier")
	}
	script := fmt.Sprintf(`tell application id %q to quit`, app.ID)
	return exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Run()
}

func (service *macOSService) find(ctx context.Context, id string) (Application, error) {
	service.mu.RLock()
	app, found := service.apps[id]
	service.mu.RUnlock()
	if found {
		return app, nil
	}
	if _, err := service.List(ctx); err != nil {
		return Application{}, err
	}
	service.mu.RLock()
	app, found = service.apps[id]
	service.mu.RUnlock()
	if !found {
		return Application{}, errors.New("application not found")
	}
	return app, nil
}

func discoverApplications(ctx context.Context) ([]Application, error) {
	home, _ := os.UserHomeDir()
	roots := []string{"/Applications", "/System/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}

	byID := make(map[string]Application)
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
			if app, err := readApplication(ctx, path); err == nil {
				if _, exists := byID[app.ID]; !exists {
					byID[app.ID] = app
				}
			}
			return filepath.SkipDir
		})
	}

	apps := make([]Application, 0, len(byID))
	for _, app := range byID {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(left, right int) bool {
		return strings.ToLower(apps[left].Name) < strings.ToLower(apps[right].Name)
	})
	return apps, nil
}

func readApplication(ctx context.Context, path string) (Application, error) {
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
		return Application{}, err
	}
	var info map[string]any
	if err := json.Unmarshal(output, &info); err != nil {
		return Application{}, err
	}
	bundleID, _ := info["CFBundleIdentifier"].(string)
	if !validBundleID.MatchString(bundleID) {
		return Application{}, errors.New("missing or invalid bundle identifier")
	}
	name := firstString(
		info["CFBundleDisplayName"],
		info["CFBundleName"],
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	)
	if name == "" {
		return Application{}, errors.New("missing application name")
	}
	return Application{ID: bundleID, Name: name, CanQuit: true, Path: path}, nil
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
