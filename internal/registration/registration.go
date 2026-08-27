package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type Payload struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Port     int    `json:"port"`
	Token    string `json:"token"`
	Version  string `json:"version"`
}

func Run(ctx context.Context, apiURL string, payload Payload) {
	endpoint := strings.TrimRight(apiURL, "/") + "/api/agent/register"
	client := &http.Client{Timeout: 5 * time.Second}
	register := func() {
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			endpoint,
			bytes.NewReader(body),
		)
		if err != nil {
			return
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			log.Printf("agent registration failed: %v", err)
			return
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			log.Printf("agent registration failed: %s", response.Status)
		}
	}

	register()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			register()
		}
	}
}

func Address(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
