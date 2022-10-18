package election

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/nais/elector/pkg/logging"
)

type Official struct {
	Logger          logrus.FieldLogger
	ElectionResults <-chan string
	ElectionAddress string
	lastResult      result
}

type result struct {
	Name       string `json:"name,omitempty"`
	LastUpdate string `json:"last_update,omitempty"`
}

func AddOfficialToManager(mgr manager.Manager, logger logrus.FieldLogger, electionResults <-chan string, electionAddress string) error {
	official := Official{
		Logger:          logger.WithField(logging.FieldComponent, "Manager"),
		ElectionResults: electionResults,
		ElectionAddress: electionAddress,
	}

	err := mgr.AddReadyzCheck("official", official.readyz)
	if err != nil {
		return fmt.Errorf("failed to add official readiness check to controller-runtime manager: %w", err)
	}

	err = mgr.Add(&official)
	if err != nil {
		return fmt.Errorf("failed to add official runnable to controller-runtime manager: %w", err)
	}

	return nil
}

func (o *Official) readyz(_ *http.Request) error {
	if o.lastResult.Name == "" {
		return fmt.Errorf("no election has run")
	}
	return nil
}

func (o *Official) Start(ctx context.Context) error {
	leaderHandler := func(w http.ResponseWriter, req *http.Request) {
		bytes, err := json.Marshal(o.lastResult)
		if err != nil {
			o.Logger.Errorf("failed to marshal JSON response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(bytes)
		if err != nil {
			o.Logger.Errorf("failed to write response: %v", err)
			return
		}
	}

	http.HandleFunc("/", leaderHandler)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		o.Logger.Infof("Starting election service on %s", o.ElectionAddress)
		err := http.ListenAndServe(o.ElectionAddress, nil)
		o.Logger.Errorf("Failed to serve: %v", err)
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case name := <-o.ElectionResults:
			o.lastResult = result{
				Name:       name,
				LastUpdate: time.Now().Format(time.RFC3339),
			}
			o.Logger.Debugf("Updated election results. Current leader: %s", name)
		}
	}
}
