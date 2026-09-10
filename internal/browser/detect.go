package browser

import (
	"encoding/json"
	"regexp"
	"strings"
)

// SafariBundleID is special-cased throughout: Safari is the one browser
// macOS never asks to advertise itself as one. It declares no
// `CFBundleURLTypes` entry for http/https — the system knows it can open
// a web page without being told — so every predicate below that leans on
// an app's own Info.plist would rule it out.
const SafariBundleID = "com.apple.Safari"

var validBundleID = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

/*
isWebBrowser decides whether an .app bundle belongs in the browser list,
from the Info.plist it declares.

Handling the `http`/`https` URL schemes is not enough on its own, and
that was the whole of the old test: plenty of applications that are not
browsers register as openers of http URLs — VLC (it streams from them),
Snagit, Parallels Desktop — and every one of them was showing up as
something to open a web page with.

So the browsing half is required too: `NSUserActivityTypes` carrying
`NSUserActivityTypeBrowsingWeb`, which is what an application declares to
say it can *be* a web browser (the same declaration that lets it appear
in System Settings' own Default web browser list, and that Handoff reads
to pass a page between devices). An app that both opens http URLs and
claims web browsing is a browser; one that only opens the URLs is an
application with an opinion about a link.
*/
func isWebBrowser(info map[string]any, bundleID string) bool {
	if strings.EqualFold(bundleID, SafariBundleID) {
		return true
	}
	return handlesHTTP(info["CFBundleURLTypes"]) &&
		browsesWeb(info["NSUserActivityTypes"])
}

// handlesHTTP reports whether the bundle declares itself an opener of
// http(s) URLs — necessary for a browser, and on its own not remotely
// sufficient (see `isWebBrowser`).
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

// browsesWeb reports whether the bundle claims it can act as a web
// browser, rather than merely open a link.
func browsesWeb(activityTypes any) bool {
	types, ok := activityTypes.([]any)
	if !ok {
		return false
	}
	for _, entry := range types {
		text, ok := entry.(string)
		if ok && strings.EqualFold(text, "NSUserActivityTypeBrowsingWeb") {
			return true
		}
	}
	return false
}

/*
defaultBrowserID reads which browser the Mac opens a link with, out of
LaunchServices' own preferences (`com.apple.launchservices.secure`, as
JSON — see the darwin service for how it's converted).

An empty answer is not a failure: the file only records a *changed*
handler, so a Mac still on its factory answer has no `http` entry at all.
The caller takes Safari for that, which is what the system would do.

The identifiers in here are LaunchServices' own, folded to lower case
("com.google.chrome" for a bundle that calls itself "com.google.Chrome"),
so every comparison against a discovered bundle id has to be
case-insensitive.
*/
func defaultBrowserID(handlers []byte) string {
	var preferences struct {
		LSHandlers []struct {
			URLScheme string `json:"LSHandlerURLScheme"`
			RoleAll   string `json:"LSHandlerRoleAll"`
		} `json:"LSHandlers"`
	}
	if err := json.Unmarshal(handlers, &preferences); err != nil {
		return ""
	}
	for _, handler := range preferences.LSHandlers {
		if strings.EqualFold(handler.URLScheme, "http") && handler.RoleAll != "" {
			return handler.RoleAll
		}
	}
	return ""
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
