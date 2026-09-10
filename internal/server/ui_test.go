package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubikyo/kbrd-agent/internal/config"
)

func newStore(t *testing.T) *config.Store {
	t.Helper()
	store, err := config.Load(filepath.Join(t.TempDir(), "agent.json"), config.Config{
		APIURL: "http://kbrd.local:8081",
		Host:   "0.0.0.0",
		Port:   8090,
		Name:   "Mac",
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return store
}

func local(request *http.Request) *http.Request {
	request.RemoteAddr = "127.0.0.1:54321"
	return request
}

func TestInterfaceRejectsRemoteClients(t *testing.T) {
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, newStore(t))
	for _, target := range []string{"/", "/v1/status", "/v1/events"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.RemoteAddr = "192.168.1.20:4444"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s: expected %d, got %d", target, http.StatusForbidden, response.Code)
		}
	}
}

func TestServesInterfaceAndStatusLocally(t *testing.T) {
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, newStore(t))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, local(httptest.NewRequest(http.MethodGet, "/", nil)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "KBRD Agent") {
		t.Fatalf("unexpected page: %d", response.Code)
	}

	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, local(httptest.NewRequest(http.MethodGet, "/v1/status", nil)))
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, statusResponse.Code)
	}
	var status struct {
		CanRestart bool `json:"canRestart"`
		Config     struct {
			APIURL string `json:"apiUrl"`
			Port   int    `json:"port"`
			Token  string `json:"token"`
		} `json:"config"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !status.CanRestart || status.Config.Port != 8090 ||
		status.Config.APIURL != "http://kbrd.local:8081" || status.Config.Token == "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestUpdatesConfigurationAndKeepsToken(t *testing.T) {
	store := newStore(t)
	token := store.Current().Token
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, store)

	body := `{"apiUrl":"http://192.168.1.10:8081/","host":"0.0.0.0","port":9100,"name":"Studio"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, local(
		httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body)),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, response.Code, response.Body)
	}
	updated := store.Current()
	if updated.APIURL != "http://192.168.1.10:8081" || updated.Port != 9100 ||
		updated.Name != "Studio" {
		t.Fatalf("unexpected config: %+v", updated)
	}
	if updated.Token != token {
		t.Fatal("le jeton doit être conservé")
	}
}

func TestRejectsInvalidConfiguration(t *testing.T) {
	store := newStore(t)
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, store)

	body := `{"apiUrl":"kbrd.local","host":"0.0.0.0","port":9100,"name":"Studio"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, local(
		httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body)),
	))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, response.Code)
	}
	if store.Current().Port != 8090 {
		t.Fatal("une configuration invalide ne doit rien modifier")
	}
}

func TestRecordsReceivedRequests(t *testing.T) {
	handler := newTestServer(&fakeApplications{}, &fakeBrowsers{}, newStore(t))

	launch := httptest.NewRequest(http.MethodPost, "/v1/applications/com.example.App/launch", nil)
	launch.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(httptest.NewRecorder(), launch)

	denied := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
	handler.ServeHTTP(httptest.NewRecorder(), denied)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, local(httptest.NewRequest(http.MethodGet, "/v1/events", nil)))
	var payload struct {
		Events []struct {
			Level   string `json:"level"`
			Path    string `json:"path"`
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(payload.Events))
	}
	if payload.Events[0].Message != "launch com.example.App" || payload.Events[0].Status != 200 {
		t.Fatalf("unexpected first event: %+v", payload.Events[0])
	}
	if payload.Events[1].Level != "error" || payload.Events[1].Status != http.StatusUnauthorized {
		t.Fatalf("unexpected second event: %+v", payload.Events[1])
	}
}

func TestRestartIsTriggeredOnce(t *testing.T) {
	calls := make(chan struct{}, 2)
	handler := New(Options{
		Applications: &fakeApplications{},
		Browsers:     &fakeBrowsers{},
		Token:        "secret",
		Config:       newStore(t),
		Restart:      func() { calls <- struct{}{} },
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, local(httptest.NewRequest(http.MethodPost, "/v1/restart", nil)))
	if response.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, response.Code)
	}
	<-calls
}
