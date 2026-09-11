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
	"time"
)

var validBundleID = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// How long a discovered list stays good for. The installed applications
// are not a fast-moving set, and every panel in KBRD-WEB that offers them
// asks for the whole list on mount — so the answer is worth keeping for
// longer than a burst of those. `find` bypasses this whenever it is asked
// for an id the list doesn't have, which is what keeps an application
// installed a minute ago reachable rather than missing until the TTL runs
// out.
const cacheTTL = 5 * time.Minute

// How many bundles are read at once. Each read is a `plutil` process, so
// the walk is bound by process spawns rather than by this machine's cores:
// a Mac with a couple of hundred applications spent almost all of its time
// waiting on them one at a time, and running a batch at once is where the
// wait actually goes. Bounded all the same — a fork per application, all
// at once, is not a kindness to the rest of the machine.
const readConcurrency = 16

type macOSService struct {
	mu   sync.Mutex
	apps map[string]Application
	// The last successful discovery, and when it landed — served as-is
	// while it is younger than `cacheTTL`.
	cached   []Application
	cachedAt time.Time
	// Closed when the discovery currently running finishes, `nil` when
	// none is. Every caller arriving while one is in flight waits on it
	// instead of starting a second one: the list is the same for all of
	// them, and it is far too expensive to build twice over (the panels
	// mount together, so this is the common case, not the rare one).
	pending chan struct{}
	// What that discovery came back with, read by everyone who waited on
	// it.
	result []Application
	err    error
}

func NewService() Service {
	return &macOSService{apps: make(map[string]Application)}
}

func (service *macOSService) List(ctx context.Context) ([]Application, error) {
	return service.snapshot(ctx, false)
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

// snapshot answers with the installed applications, discovering them only
// when it has to: `force` skips the cached answer (but still joins a
// discovery already running, which is fresh by definition), and a single
// discovery ever runs at a time.
func (service *macOSService) snapshot(
	ctx context.Context,
	force bool,
) ([]Application, error) {
	service.mu.Lock()
	if !force && service.cached != nil &&
		time.Since(service.cachedAt) < cacheTTL {
		cached := service.cached
		service.mu.Unlock()
		return cached, nil
	}
	if pending := service.pending; pending != nil {
		service.mu.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		service.mu.Lock()
		apps, err := service.result, service.err
		service.mu.Unlock()
		return apps, err
	}
	pending := make(chan struct{})
	service.pending = pending
	service.mu.Unlock()

	apps, err := discoverApplications(ctx)

	service.mu.Lock()
	service.result, service.err = apps, err
	if err == nil {
		service.cached, service.cachedAt = apps, time.Now()
		service.apps = make(map[string]Application, len(apps))
		for _, app := range apps {
			service.apps[app.ID] = app
		}
	}
	service.pending = nil
	close(pending)
	service.mu.Unlock()
	return apps, err
}

func (service *macOSService) find(ctx context.Context, id string) (Application, error) {
	service.mu.Lock()
	app, found := service.apps[id]
	service.mu.Unlock()
	if found {
		return app, nil
	}
	// An id the list doesn't know is the one case worth paying for a fresh
	// discovery: it is what an application installed since the last one
	// looks like.
	if _, err := service.snapshot(ctx, true); err != nil {
		return Application{}, err
	}
	service.mu.Lock()
	app, found = service.apps[id]
	service.mu.Unlock()
	if !found {
		return Application{}, errors.New("application not found")
	}
	return app, nil
}

func discoverApplications(ctx context.Context) ([]Application, error) {
	paths, err := applicationBundles(ctx)
	if err != nil {
		return nil, err
	}

	// Filled by index rather than appended to, so the order the bundles
	// were walked in survives the workers — that order is what "the first
	// root wins" below means.
	read := make([]Application, len(paths))
	ok := make([]bool, len(paths))

	workers := readConcurrency
	if len(paths) < workers {
		workers = len(paths)
	}
	jobs := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				// A bundle that can't be read is simply not an
				// application this agent can offer — the same silence
				// the serial walk kept.
				if app, err := readApplication(ctx, paths[index]); err == nil {
					read[index], ok[index] = app, true
				}
			}
		}()
	}
	for index := range paths {
		if ctx.Err() != nil {
			break
		}
		jobs <- index
	}
	close(jobs)
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// One entry per bundle identifier, the first root to carry it winning
	// — /Applications over /System/Applications over the user's own.
	byID := make(map[string]struct{}, len(paths))
	apps := make([]Application, 0, len(paths))
	for index := range paths {
		if !ok[index] {
			continue
		}
		app := read[index]
		if _, exists := byID[app.ID]; exists {
			continue
		}
		byID[app.ID] = struct{}{}
		apps = append(apps, app)
	}
	sort.Slice(apps, func(left, right int) bool {
		return strings.ToLower(apps[left].Name) < strings.ToLower(apps[right].Name)
	})
	return apps, nil
}

// applicationBundles walks the three places applications live for their
// `.app` directories. Cheap on its own — directory reads, no processes —
// which is why it is kept apart from reading them: the paths are collected
// first so every read after it can run alongside the others.
func applicationBundles(ctx context.Context) ([]string, error) {
	home, _ := os.UserHomeDir()
	roots := []string{"/Applications", "/System/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}

	var paths []string
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil || !entry.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".app") {
				return nil
			}
			paths = append(paths, path)
			// A bundle is a leaf as far as this walk is concerned: an
			// application shipped inside another one is that
			// application's own business.
			return filepath.SkipDir
		})
	}
	return paths, nil
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
