package server

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ubikyo/kbrd-agent/src/internal/config"
	"github.com/ubikyo/kbrd-agent/src/internal/events"
)

//go:embed ui/index.html
var page []byte

// localOnly réserve l'interface de configuration aux clients de la machine.
// L'agent écoute sur toutes les interfaces pour que KBRD-API puisse le
// joindre, mais seul le Mac lui-même doit pouvoir modifier ses réglages.
func localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		address := net.ParseIP(remoteHost(request))
		if address == nil || !address.IsLoopback() {
			writeError(response, request, http.StatusForbidden,
				"interface accessible depuis cette machine uniquement")
			return
		}
		next(response, request)
	}
}

func remoteHost(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func (server *Server) page(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(page)
}

func (server *Server) status(response http.ResponseWriter, request *http.Request) {
	if server.config == nil {
		writeError(response, request, http.StatusServiceUnavailable,
			"configuration indisponible")
		return
	}
	current := server.config.Current()
	writeJSON(response, http.StatusOK, map[string]any{
		"version":       server.version,
		"platform":      "macos",
		"configPath":    server.config.Path(),
		"startedAt":     server.startedAt.Format(time.RFC3339),
		"uptimeSeconds": int64(time.Since(server.startedAt).Seconds()),
		"canRestart":    server.restart != nil,
		"config": map[string]any{
			"apiUrl": current.APIURL,
			"host":   current.Host,
			"port":   current.Port,
			"name":   current.Name,
			"token":  current.Token,
		},
	})
}

func (server *Server) eventList(response http.ResponseWriter, request *http.Request) {
	since, _ := strconv.ParseUint(request.URL.Query().Get("since"), 10, 64)
	writeJSON(response, http.StatusOK, map[string]any{
		"events": server.events.List(since),
	})
}

func (server *Server) configUpdate(response http.ResponseWriter, request *http.Request) {
	if server.config == nil {
		writeError(response, request, http.StatusServiceUnavailable,
			"configuration indisponible")
		return
	}
	var payload struct {
		APIURL string `json:"apiUrl"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "requête illisible")
		return
	}
	updated, err := server.config.Update(config.Config{
		APIURL: payload.APIURL,
		Host:   payload.Host,
		Port:   payload.Port,
		Name:   payload.Name,
	})
	if err != nil {
		writeError(response, request, http.StatusBadRequest, err.Error())
		return
	}
	events.Annotate(request.Context(), events.LevelInfo,
		"configuration enregistrée (redémarrage requis)")
	writeJSON(response, http.StatusOK, map[string]any{
		"ok":              true,
		"restartRequired": true,
		"config": map[string]any{
			"apiUrl": updated.APIURL,
			"host":   updated.Host,
			"port":   updated.Port,
			"name":   updated.Name,
			"token":  updated.Token,
		},
	})
}

func (server *Server) restartAgent(response http.ResponseWriter, request *http.Request) {
	if server.restart == nil {
		writeError(response, request, http.StatusNotImplemented,
			"redémarrage indisponible dans ce mode d'exécution")
		return
	}
	events.Annotate(request.Context(), events.LevelInfo, "redémarrage demandé")
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
	// Laisser la réponse partir avant de couper le processus : launchd relance
	// l'agent grâce à KeepAlive.
	go func() {
		time.Sleep(250 * time.Millisecond)
		server.restart()
	}()
}
