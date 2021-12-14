package election

import (
	"context"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"net/http"
	"time"
)

type Manager struct {
	Logger          logrus.FieldLogger
	ElectionResults <-chan string
	ElectionAddress string
	lastResult      result
}

type result struct {
	Name       string `json:"name,omitempty"`
	LastUpdate string `json:"last_update,omitempty"`
}

func (m *Manager) Start(ctx context.Context) error {
	leaderHandler := func(w http.ResponseWriter, req *http.Request) {
		bytes, err := json.Marshal(m.lastResult)
		if err != nil {
			m.Logger.Errorf("failed to marshal JSON response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write(bytes)
		if err != nil {
			m.Logger.Errorf("failed to write response: %v", err)
			return
		}
	}

	http.HandleFunc("/", leaderHandler)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		m.Logger.Infof("Starting election service on %s", m.ElectionAddress)
		err := http.ListenAndServe(m.ElectionAddress, nil)
		m.Logger.Errorf("Failed to serve: %v", err)
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case name := <-m.ElectionResults:
			m.lastResult = result{
				Name:       name,
				LastUpdate: time.Now().Format(time.RFC3339),
			}
			m.Logger.Debugf("Updated election results. Current leader: %s", name)
		}
	}
}
