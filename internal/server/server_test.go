package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ubikyo/kbrd-agent/internal/application"
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

func TestApplicationsRequireAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
	response := httptest.NewRecorder()
	New(&fakeApplications{}, "secret").ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestListsAndLaunchesApplications(t *testing.T) {
	applications := &fakeApplications{}
	handler := New(applications, "secret")
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
