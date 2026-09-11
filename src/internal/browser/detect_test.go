package browser

import "testing"

// The Info.plist keys each case turns on, as `plutil -convert json` hands
// them over: `any` values out of a decoded map, not typed structs.
func plist(urlSchemes []string, activityTypes []string) map[string]any {
	info := map[string]any{}
	if urlSchemes != nil {
		schemes := make([]any, 0, len(urlSchemes))
		for _, scheme := range urlSchemes {
			schemes = append(schemes, scheme)
		}
		info["CFBundleURLTypes"] = []any{
			map[string]any{"CFBundleURLSchemes": schemes},
		}
	}
	if activityTypes != nil {
		types := make([]any, 0, len(activityTypes))
		for _, activity := range activityTypes {
			types = append(types, activity)
		}
		info["NSUserActivityTypes"] = types
	}
	return info
}

func TestAcceptsWebBrowsers(t *testing.T) {
	browsers := map[string]map[string]any{
		"org.mozilla.firefox": plist(
			[]string{"http", "https"},
			[]string{"NSUserActivityTypeBrowsingWeb"},
		),
		"com.google.Chrome": plist(
			[]string{"http", "https", "ftp"},
			[]string{"NSUserActivityTypeBrowsingWeb"},
		),
		// Safari declares neither key — the system knows what it is.
		SafariBundleID: {},
	}
	for bundleID, info := range browsers {
		if !isWebBrowser(info, bundleID) {
			t.Errorf("expected %s to be a browser", bundleID)
		}
	}
}

// The applications that were filling the list up: each one registers as
// an opener of http URLs without ever claiming to be a browser.
func TestRejectsHTTPOpenersThatAreNotBrowsers(t *testing.T) {
	others := map[string]map[string]any{
		"org.videolan.vlc":              plist([]string{"http", "https", "rtsp"}, nil),
		"com.TechSmith.Snagit2024":      plist([]string{"http"}, nil),
		"com.parallels.desktop.console": plist([]string{"http", "https"}, nil),
		// Claims browsing but opens no web URL: a reader handling its own
		// scheme, not something to send a page to.
		"com.example.reader": plist(
			[]string{"example"},
			[]string{"NSUserActivityTypeBrowsingWeb"},
		),
		"com.example.plain": {},
	}
	for bundleID, info := range others {
		if isWebBrowser(info, bundleID) {
			t.Errorf("expected %s not to be a browser", bundleID)
		}
	}
}

func TestReadsDefaultBrowserFromLaunchServices(t *testing.T) {
	handlers := []byte(`{"LSHandlers":[
		{"LSHandlerURLScheme":"mailto","LSHandlerRoleAll":"com.apple.mail"},
		{"LSHandlerURLScheme":"http","LSHandlerRoleAll":"com.google.chrome"},
		{"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.google.chrome"}
	]}`)
	if id := defaultBrowserID(handlers); id != "com.google.chrome" {
		t.Fatalf("unexpected default: %q", id)
	}
}

// A Mac that has never been given a different browser records no `http`
// handler at all, and an unreadable file is the same non-answer. Both
// leave the caller to fall back to Safari.
func TestDefaultBrowserFallsBackToNothing(t *testing.T) {
	for name, handlers := range map[string][]byte{
		"no http entry": []byte(
			`{"LSHandlers":[{"LSHandlerURLScheme":"mailto","LSHandlerRoleAll":"com.apple.mail"}]}`,
		),
		"no handlers": []byte(`{}`),
		"not json":    []byte("\x00binary"),
		"empty role":  []byte(`{"LSHandlers":[{"LSHandlerURLScheme":"http","LSHandlerRoleAll":""}]}`),
	} {
		if id := defaultBrowserID(handlers); id != "" {
			t.Errorf("%s: expected no default, got %q", name, id)
		}
	}
}
