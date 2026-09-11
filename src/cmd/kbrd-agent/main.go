package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ubikyo/kbrd-agent/internal/application"
	"github.com/ubikyo/kbrd-agent/internal/browser"
	"github.com/ubikyo/kbrd-agent/internal/config"
	"github.com/ubikyo/kbrd-agent/internal/events"
	"github.com/ubikyo/kbrd-agent/internal/registration"
	"github.com/ubikyo/kbrd-agent/internal/server"
)

const version = "1.0.0"

func main() {
	host := flag.String("host", env("KBRD_AGENT_HOST", "0.0.0.0"), "listen host")
	port := flag.Int("port", envInt("KBRD_AGENT_PORT", 8090), "listen port")
	apiURL := flag.String(
		"api-url",
		env("KBRD_API_URL", "http://kbrd.local:8081"),
		"KBRD API base URL",
	)
	name := flag.String("name", env("KBRD_AGENT_NAME", hostname()), "agent name")
	configPath := flag.String("config", config.DefaultPath(), "settings file path")
	flag.Parse()

	// Les options ci-dessus ne servent qu'à créer le fichier de réglages au
	// premier démarrage : ensuite le fichier, modifiable depuis l'interface
	// web locale, fait autorité.
	settings, err := config.Load(*configPath, config.Config{
		APIURL: *apiURL,
		Host:   *host,
		Port:   *port,
		Name:   *name,
	})
	if err != nil {
		log.Fatal(err)
	}
	current := settings.Current()

	recorder := events.NewRecorder(500)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	restart := make(chan struct{}, 1)
	applications := application.NewService()
	httpServer := &http.Server{
		Addr: registration.Address(current.Host, current.Port),
		Handler: server.New(server.Options{
			Applications: applications,
			Browsers:     browser.NewService(),
			Token:        current.Token,
			Events:       recorder,
			Config:       settings,
			Version:      version,
			Restart: func() {
				select {
				case restart <- struct{}{}:
				default:
				}
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	// Recenser les applications installées coûte une lecture de plist par
	// bundle : plusieurs secondes au premier appel. Le faire dès le
	// démarrage remplit le cache du service avant que l'interface ne le
	// demande, si bien que le premier panneau ouvert répond déjà de
	// mémoire. L'échec ne se signale pas : la découverte sera refaite à la
	// demande.
	go func() {
		warmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if _, err := applications.List(warmCtx); err != nil {
			recorder.Note(events.LevelInfo, "applications",
				"préchargement impossible ("+err.Error()+")")
		}
	}()
	go registration.Run(ctx, current.APIURL, registration.Payload{
		Name:     current.Name,
		Platform: "macos",
		Port:     current.Port,
		Token:    current.Token,
		Version:  version,
	}, func(level, message string) {
		recorder.Note(level, "registration", message)
	})
	go func() {
		recorder.Note(events.LevelInfo, "agent",
			"écoute sur "+httpServer.Addr+" — API "+current.APIURL)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	select {
	case <-ctx.Done():
		shutdown(httpServer)
	case <-restart:
		stop()
		shutdown(httpServer)
		relaunch(recorder)
	}
}

func shutdown(httpServer *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

// relaunch remplace le processus courant par une nouvelle instance, ce qui
// recharge les réglages sans dépendre de launchd. Si l'appel échoue, sortir
// suffit : le LaunchAgent est déclaré KeepAlive.
func relaunch(recorder *events.Recorder) {
	recorder.Note(events.LevelInfo, "agent", "redémarrage de l'agent")
	executable, err := os.Executable()
	if err == nil {
		err = syscall.Exec(executable, os.Args, os.Environ())
	}
	recorder.Note(events.LevelError, "agent",
		"redémarrage direct impossible ("+err.Error()+"), arrêt du processus")
	os.Exit(0)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "Mac"
	}
	return name
}
