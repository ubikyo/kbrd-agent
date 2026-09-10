// Package events conserve en mémoire les derniers évènements de l'agent afin
// que l'interface web locale puisse les afficher sans lire le fichier de log.
package events

import (
	"context"
	"log"
	"sync"
	"time"
)

// Level qualifie un évènement pour la mise en forme dans l'interface.
const (
	LevelInfo  = "info"
	LevelError = "error"
)

// Event décrit une requête reçue ou une action interne de l'agent.
type Event struct {
	ID       uint64 `json:"id"`
	Time     string `json:"time"`
	Level    string `json:"level"`
	Source   string `json:"source"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
	Status   int    `json:"status,omitempty"`
	Duration int64  `json:"durationMs,omitempty"`
	Remote   string `json:"remote,omitempty"`
	Message  string `json:"message,omitempty"`
}

// Recorder est un tampon circulaire sûr en accès concurrent.
type Recorder struct {
	mu       sync.RWMutex
	capacity int
	items    []Event
	sequence uint64
}

// NewRecorder crée un journal conservant au plus capacity évènements.
func NewRecorder(capacity int) *Recorder {
	if capacity < 1 {
		capacity = 1
	}
	return &Recorder{capacity: capacity, items: make([]Event, 0, capacity)}
}

// Add horodate puis enregistre un évènement, et le recopie dans le log fichier
// pour que ~/Library/Logs/KBRD/agent.log reste la trace durable.
func (recorder *Recorder) Add(event Event) {
	recorder.mu.Lock()
	recorder.sequence++
	event.ID = recorder.sequence
	event.Time = time.Now().Format(time.RFC3339)
	if event.Level == "" {
		event.Level = LevelInfo
	}
	if len(recorder.items) == recorder.capacity {
		copy(recorder.items, recorder.items[1:])
		recorder.items[len(recorder.items)-1] = event
	} else {
		recorder.items = append(recorder.items, event)
	}
	recorder.mu.Unlock()
	log.Print(format(event))
}

// Note enregistre un évènement interne (démarrage, configuration, redémarrage).
func (recorder *Recorder) Note(level, source, message string) {
	recorder.Add(Event{Level: level, Source: source, Message: message})
}

// List renvoie les évènements postérieurs à since, du plus ancien au plus
// récent. Passer 0 pour tout obtenir.
func (recorder *Recorder) List(since uint64) []Event {
	recorder.mu.RLock()
	defer recorder.mu.RUnlock()
	result := make([]Event, 0, len(recorder.items))
	for _, event := range recorder.items {
		if event.ID > since {
			result = append(result, event)
		}
	}
	return result
}

func format(event Event) string {
	if event.Method != "" {
		line := event.Method + " " + event.Path
		if event.Message != "" {
			line += " — " + event.Message
		}
		return line
	}
	return event.Message
}

type contextKey struct{}

type annotation struct {
	mu      sync.Mutex
	level   string
	message string
}

// WithAnnotation attache à la requête un emplacement que les gestionnaires
// peuvent renseigner pour préciser l'évènement journalisé par le middleware.
func WithAnnotation(ctx context.Context) (context.Context, func() (string, string)) {
	slot := &annotation{}
	return context.WithValue(ctx, contextKey{}, slot), func() (string, string) {
		slot.mu.Lock()
		defer slot.mu.Unlock()
		return slot.level, slot.message
	}
}

// Annotate précise l'évènement de la requête en cours. Sans annotation, seuls
// la méthode, le chemin et le code de réponse sont journalisés.
func Annotate(ctx context.Context, level, message string) {
	slot, ok := ctx.Value(contextKey{}).(*annotation)
	if !ok {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	slot.level = level
	slot.message = message
}
