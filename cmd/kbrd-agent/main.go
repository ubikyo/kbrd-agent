package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	flag.Parse()

	token, err := randomToken()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:              registration.Address(*host, *port),
		Handler:           server.New(application.NewService(), browser.NewService(), token),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go registration.Run(ctx, *apiURL, registration.Payload{
		Name:     *name,
		Platform: "macos",
		Port:     *port,
		Token:    token,
		Version:  version,
	})
	go func() {
		log.Printf("KBRD Agent listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
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
