package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ubikyo/kbrd-agent/internal/application"
)

type Server struct {
	applications application.Service
	token        string
}

func New(applications application.Service, token string) http.Handler {
	server := &Server{applications: applications, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/applications", server.auth(server.list))
	mux.HandleFunc("POST /v1/applications/", server.auth(server.action))
	return requestLog(mux)
}

func (server *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(server.token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(server.token)) != 1 {
			writeError(response, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(response, request)
	}
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":       true,
		"platform": "macos",
		"version":  "1.0.0",
	})
}

func (server *Server) list(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	apps, err := server.applications.List(ctx)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, apps)
}

func (server *Server) action(response http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, "/v1/applications/")
	separator := strings.LastIndexByte(remainder, '/')
	if separator < 1 {
		writeError(response, http.StatusNotFound, "not found")
		return
	}
	id, err := url.PathUnescape(remainder[:separator])
	if err != nil || id == "" {
		writeError(response, http.StatusBadRequest, "invalid application id")
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
		writeError(response, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(response, status, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		log.Printf("%s %s", request.Method, request.URL.Path)
		next.ServeHTTP(response, request)
	})
}
