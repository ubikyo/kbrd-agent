package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ubikyo/kbrd-agent/src/internal/application"
	"github.com/ubikyo/kbrd-agent/src/internal/browser"
	"github.com/ubikyo/kbrd-agent/src/internal/config"
	"github.com/ubikyo/kbrd-agent/src/internal/events"
)

// ConfigStore expose au serveur les réglages persistés, éditables depuis
// l'interface web locale.
type ConfigStore interface {
	Path() string
	Current() config.Config
	Update(config.Config) (config.Config, error)
}

// Options rassemble les dépendances du serveur HTTP de l'agent.
type Options struct {
	Applications application.Service
	Browsers     browser.Service
	Token        string
	Events       *events.Recorder
	Config       ConfigStore
	// Restart est appelée après la réponse à POST /v1/restart. Laissée nulle,
	// la route répond que le redémarrage n'est pas disponible.
	Restart func()
	// Version est renvoyée par /v1/health et affichée par l'interface.
	Version string
}

type Server struct {
	applications application.Service
	browsers     browser.Service
	token        string
	events       *events.Recorder
	config       ConfigStore
	restart      func()
	version      string
	startedAt    time.Time
}

// New construit le routeur de l'agent. Les routes `/v1/...` métier sont
// protégées par le jeton partagé avec KBRD-API ; l'interface de configuration
// n'est servie qu'aux clients de la machine elle-même.
func New(options Options) http.Handler {
	if options.Events == nil {
		options.Events = events.NewRecorder(1)
	}
	server := &Server{
		applications: options.Applications,
		browsers:     options.Browsers,
		token:        options.Token,
		events:       options.Events,
		config:       options.Config,
		restart:      options.Restart,
		version:      options.Version,
		startedAt:    time.Now(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/applications", server.auth(server.list))
	mux.HandleFunc("POST /v1/applications/", server.auth(server.action))
	mux.HandleFunc("GET /v1/browsers", server.auth(server.browserList))
	mux.HandleFunc("POST /v1/browsers/", server.auth(server.browserOpen))
	mux.HandleFunc("GET /{$}", localOnly(server.page))
	mux.HandleFunc("GET /v1/status", localOnly(server.status))
	mux.HandleFunc("GET /v1/events", localOnly(server.eventList))
	mux.HandleFunc("PUT /v1/config", localOnly(server.configUpdate))
	mux.HandleFunc("POST /v1/restart", localOnly(server.restartAgent))
	return record(server.events, mux)
}

func (server *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(server.token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(server.token)) != 1 {
			writeError(response, request, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(response, request)
	}
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":       true,
		"platform": "macos",
		"version":  server.version,
	})
}

func (server *Server) list(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	apps, err := server.applications.List(ctx)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, apps)
}

func (server *Server) action(response http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, "/v1/applications/")
	separator := strings.LastIndexByte(remainder, '/')
	if separator < 1 {
		writeError(response, request, http.StatusNotFound, "not found")
		return
	}
	id, err := url.PathUnescape(remainder[:separator])
	if err != nil || id == "" {
		writeError(response, request, http.StatusBadRequest, "invalid application id")
		return
	}
	action := remainder[separator+1:]
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	switch action {
	case "launch":
		err = server.applications.Launch(ctx, id)
	case "quit":
		err = server.applications.Quit(ctx, id)
	default:
		writeError(response, request, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(response, request, status, err.Error())
		return
	}
	events.Annotate(request.Context(), events.LevelInfo, action+" "+id)
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) browserList(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	browsers, err := server.browsers.List(ctx)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, browsers)
}

func (server *Server) browserOpen(response http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, "/v1/browsers/")
	separator := strings.LastIndexByte(remainder, '/')
	if separator < 1 || remainder[separator+1:] != "open" {
		writeError(response, request, http.StatusNotFound, "not found")
		return
	}
	id, err := url.PathUnescape(remainder[:separator])
	if err != nil || id == "" {
		writeError(response, request, http.StatusBadRequest, "invalid browser id")
		return
	}

	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.URL == "" {
		writeError(response, request, http.StatusBadRequest, "missing url")
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	if err := server.browsers.Open(ctx, id, payload.URL); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(response, request, status, err.Error())
		return
	}
	events.Annotate(request.Context(), events.LevelInfo, "open "+id+" "+payload.URL)
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	message string,
) {
	events.Annotate(request.Context(), events.LevelError, message)
	writeJSON(response, status, map[string]string{"error": message})
}

// statusWriter mémorise le code de réponse pour le journal.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

// record journalise chaque requête reçue. Les relevés de l'interface elle-même
// sont ignorés pour ne pas noyer le journal sous son propre rafraîchissement.
func record(recorder *events.Recorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx, annotation := events.WithAnnotation(request.Context())
		request = request.WithContext(ctx)
		writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(writer, request)
		if isPolling(request) {
			return
		}
		level, message := annotation()
		if level == "" {
			level = events.LevelInfo
		}
		recorder.Add(events.Event{
			Level:    level,
			Source:   "http",
			Method:   request.Method,
			Path:     request.URL.Path,
			Status:   writer.status,
			Duration: time.Since(started).Milliseconds(),
			Remote:   remoteHost(request),
			Message:  message,
		})
	})
}

func isPolling(request *http.Request) bool {
	if request.Method != http.MethodGet {
		return false
	}
	switch request.URL.Path {
	case "/", "/v1/events", "/v1/status":
		return true
	}
	return false
}
