package election

import (
	"context"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"net/http"
)

type Manager struct {
	Name            string `json:"name,omitempty"`
	logger          logrus.FieldLogger
	electionResults <-chan string
}

func NewManager(logger logrus.FieldLogger, electionResults <-chan string) Manager {
	return Manager{
		logger:          logger,
		electionResults: electionResults,
	}
}

func (m *Manager) Run(ctx context.Context) error {
	leaderHandler := func(w http.ResponseWriter, req *http.Request) {
		bytes, err := json.Marshal(m)
		if err != nil {
			m.logger.Errorf("failed to marshal JSON response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write(bytes)
		if err != nil {
			m.logger.Errorf("failed to write response: %v", err)
			return
		}
	}

	http.HandleFunc("/", leaderHandler)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		m.logger.Errorf("Failed to serve: %v", err)
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m.Name = <-m.electionResults:
		}
	}
}
