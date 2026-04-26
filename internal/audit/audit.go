package audit

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/david/jenkins-mcp/internal/config"
)

type Event struct {
	Time       time.Time `json:"time"`
	Controller string    `json:"controller"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Outcome    string    `json:"outcome"`
	Error      string    `json:"error,omitempty"`
}
type Logger struct {
	path string
	mu   sync.Mutex
}

func New(cfg config.AuditConfig) (*Logger, error) { return &Logger{path: cfg.Path}, nil }
func (l *Logger) Emit(event Event) error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}
