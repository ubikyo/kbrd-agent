package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubikyo/kbrd-agent/internal/application"
	"github.com/ubikyo/kbrd-agent/internal/browser"
	"github.com/ubikyo/kbrd-agent/internal/events"
)

type fakeApplications struct {
	lastAction string
	lastID     string
}

func (service *fakeApplications) List(context.Context) ([]application.Application, error) {
	return []application.Application{{ID: "com.example.App", Name: "Example", CanQuit: true}}, nil
}

func (service *fakeApplications) Launch(_ context.Context, id string) error {
	service.lastAction = "launch"
	service.lastID = id
	return nil
}

func (service *fakeApplications) Quit(_ context.Context, id string) error {
	service.lastAction = "quit"
	service.lastID = id
	return nil
}

type fakeBrowsers struct {
	lastID  string
	lastURL string
}

func (service *fakeBrowsers) List(context.Context) ([]browser.Browser, error) {
	return []browser.Browser{{ID: "com.example.Browser", Name: "Example Browser"}}, nil
}

func (service *fakeBrowsers) Open(_ context.Context, id string, url string) error {
	service.lastID = id
	service.lastURL = url
	return nil
}

func newTestServer(
	applications application.Service,
	browsers browser.Service,
	store ConfigStore,
) http.Handler {
	return New(Options{
		Applications: applications,
		Browsers:     browsers,
		Token:        "secret",
		Events:       events.NewRecorder(50),
		Config:       store,
		Version:      "test",
		Restart:      func() {},
	})
}

func TestApplicationsRequireAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
	response := httptest.NewRecorder()
	newTestServer(&fakeApplications{}, &fakeBrowsers{}, nil).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestListsAndLaunchesApplications(t *testing.T) {
	applications := &fakeApplications{}
	handler := newTestServer(applications, &fakeBrowsers{}, nil)
	list := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
	list.Header.Set("Authorization", "Bearer secret")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, listResponse.Code)
	}

	launch := httptest.NewRequest(
		http.MethodPost,
		"/v1/applications/com.example.App/launch",
		nil,
	)
	launch.Header.Set("Authorization", "Bearer secret")
	launchResponse := httptest.NewRecorder()
	handler.ServeHTTP(launchResponse, launch)
	if launchResponse.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, launchResponse.Code)
	}
	if applications.lastAction != "launch" || applications.lastID != "com.example.App" {
		t.Fatalf("unexpected action: %s %s", applications.lastAction, applications.lastID)
	}
}

func TestListsAndOpensBrowsers(t *testing.T) {
	browsers := &fakeBrowsers{}
	handler := newTestServer(&fakeApplications{}, browsers, nil)

	list := httptest.NewRequest(http.MethodGet, "/v1/browsers", nil)
	list.Header.Set("Authorization", "Bearer secret")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, listResponse.Code)
	}

	open := httptest.NewRequest(
		http.MethodPost,
		"/v1/browsers/com.example.Browser/open",
		strings.NewReader(`{"url":"https://example.com"}`),
	)
	open.Header.Set("Authorization", "Bearer secret")
	openResponse := httptest.NewRecorder()
	handler.ServeHTTP(openResponse, open)
	if openResponse.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, openResponse.Code)
	}
	if browsers.lastID != "com.example.Browser" || browsers.lastURL != "https://example.com" {
		t.Fatalf("unexpected open: %s %s", browsers.lastID, browsers.lastURL)
	}
}

func TestOpenBrowserRejectsMissingURL(t *testing.T) {
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, nil)

	open := httptest.NewRequest(
		http.MethodPost,
		"/v1/browsers/com.example.Browser/open",
		strings.NewReader(`{}`),
	)
	open.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, open)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, response.Code)
	}
}
