// Package config persiste les réglages de l'agent modifiés depuis l'interface
// web locale. Le fichier fait autorité : il l'emporte sur les options de la
// ligne de commande et sur les variables d'environnement, qui ne servent qu'à
// l'initialiser au premier démarrage.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config regroupe les réglages modifiables de l'agent. Token n'est pas
// modifiable depuis l'interface : il est généré au premier démarrage puis
// conservé pour que l'enregistrement auprès de KBRD-API reste stable d'un
// redémarrage à l'autre.
type Config struct {
	APIURL string `json:"apiUrl"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Name   string `json:"name"`
	Token  string `json:"token"`
}

// Validate refuse les réglages qui empêcheraient l'agent de redémarrer.
func (config Config) Validate() error {
	parsed, err := url.Parse(strings.TrimSpace(config.APIURL))
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("l'URL de KBRD-API doit être de la forme http://hote:port")
	}
	if strings.TrimSpace(config.Host) == "" {
		return errors.New("l'adresse d'écoute est obligatoire")
	}
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("le port d'écoute doit être compris entre 1 et 65535")
	}
	if strings.TrimSpace(config.Name) == "" {
		return errors.New("le nom de l'agent est obligatoire")
	}
	return nil
}

func (config Config) normalized() Config {
	config.APIURL = strings.TrimRight(strings.TrimSpace(config.APIURL), "/")
	config.Host = strings.TrimSpace(config.Host)
	config.Name = strings.TrimSpace(config.Name)
	return config
}

// Store lit et écrit la configuration sur disque de façon concurrente.
type Store struct {
	mu      sync.RWMutex
	path    string
	current Config
}

// DefaultPath renvoie ~/Library/Application Support/KBRD/agent.json sur macOS,
// et l'équivalent XDG ailleurs pour permettre le développement sous Linux.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "agent.json"
	}
	return filepath.Join(dir, "KBRD", "agent.json")
}

// Load ouvre le fichier de configuration. S'il est absent ou illisible, les
// valeurs par défaut fournies sont utilisées puis écrites sur disque. Un jeton
// est généré si le fichier n'en contient pas encore.
func Load(path string, defaults Config) (*Store, error) {
	store := &Store{path: path, current: defaults.normalized()}
	content, err := os.ReadFile(path)
	switch {
	case err == nil:
		var stored Config
		if err := json.Unmarshal(content, &stored); err != nil {
			return nil, fmt.Errorf("configuration illisible (%s): %w", path, err)
		}
		store.current = merge(store.current, stored)
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("configuration illisible (%s): %w", path, err)
	}
	if store.current.Token == "" {
		token, err := randomToken()
		if err != nil {
			return nil, err
		}
		store.current.Token = token
	}
	if err := store.current.Validate(); err != nil {
		return nil, err
	}
	if err := store.write(store.current); err != nil {
		return nil, err
	}
	return store, nil
}

// Path renvoie l'emplacement du fichier, affiché par l'interface.
func (store *Store) Path() string {
	return store.path
}

// Current renvoie une copie des réglages en vigueur.
func (store *Store) Current() Config {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.current
}

// Update valide puis enregistre de nouveaux réglages. Le jeton en place est
// conservé : il n'est pas modifiable depuis l'interface.
func (store *Store) Update(next Config) (Config, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	next = next.normalized()
	next.Token = store.current.Token
	if err := next.Validate(); err != nil {
		return store.current, err
	}
	if err := store.write(next); err != nil {
		return store.current, err
	}
	store.current = next
	return next, nil
}

// write remplace le fichier de façon atomique pour ne jamais laisser une
// configuration tronquée derrière un redémarrage.
func (store *Store) write(value Config) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := store.path + ".tmp"
	if err := os.WriteFile(temporary, append(content, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, store.path)
}

func merge(defaults, stored Config) Config {
	if stored.APIURL != "" {
		defaults.APIURL = strings.TrimRight(strings.TrimSpace(stored.APIURL), "/")
	}
	if stored.Host != "" {
		defaults.Host = strings.TrimSpace(stored.Host)
	}
	if stored.Port > 0 {
		defaults.Port = stored.Port
	}
	if stored.Name != "" {
		defaults.Name = strings.TrimSpace(stored.Name)
	}
	if stored.Token != "" {
		defaults.Token = stored.Token
	}
	return defaults
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
