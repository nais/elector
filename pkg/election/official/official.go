package official

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nais/elector/pkg/logging"
	"github.com/sirupsen/logrus"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"time"
)

type official struct {
	Logger          logrus.FieldLogger
	ElectionResults <-chan string
	ElectionAddress string
	lastResult      result
	sseSubscribers  []chan<- result
}

type result struct {
	Name       string `json:"name,omitempty"`
	LastUpdate string `json:"last_update,omitempty"`
}

func (o *official) readyz(_ *http.Request) error {
	if o.lastResult.Name == "" {
		return fmt.Errorf("no election has run")
	}
	return nil
}

func (o *official) leaderHandler(w http.ResponseWriter, _ *http.Request) {
	bytes, done := o.marshalResult(w, o.lastResult)
	if done {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, err := w.Write(bytes)
	if err != nil {
		o.Logger.Errorf("failed to write response: %v", err)
		return
	}
}

func (o *official) marshalResult(w http.ResponseWriter, lastResult result) ([]byte, bool) {
	bytes, err := json.Marshal(lastResult)
	if err != nil {
		o.Logger.Errorf("failed to marshal JSON response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return nil, true
	}
	return bytes, false
}

func (o *official) sseHandler(ctx context.Context) func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan result)
		defer close(ch)
		o.sseSubscribers = append(o.sseSubscribers, ch)

		bytes, done := o.marshalResult(w, o.lastResult)
		if done {
			return
		}

		fmt.Fprintf(w, "data: %s\n\n", bytes)
		w.(http.Flusher).Flush()

		for {
			select {
			case <-ctx.Done():
				return
			case r := <-ch:
				bytes, done = o.marshalResult(w, r)
				if done {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", bytes)
				w.(http.Flusher).Flush()
			}
		}
	}
}

func (o *official) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)

	http.HandleFunc("/", o.leaderHandler)
	http.HandleFunc("/sse", o.sseHandler(ctx))

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
			for _, ch := range o.sseSubscribers {
				ch <- o.lastResult
			}
			o.Logger.Debugf("Updated election results. Current leader: %s", name)
		}
	}
}

func AddOfficialToManager(mgr manager.Manager, logger logrus.FieldLogger, electionResults <-chan string, electionAddress string) error {
	o := &official{
		Logger:          logger.WithField(logging.FieldComponent, "Manager"),
		ElectionResults: electionResults,
		ElectionAddress: electionAddress,
		sseSubscribers:  make([]chan<- result, 0),
	}

	err := mgr.AddReadyzCheck("official", o.readyz)
	if err != nil {
		return fmt.Errorf("failed to add official readiness check to controller-runtime manager: %w", err)
	}

	err = mgr.Add(o)
	if err != nil {
		return fmt.Errorf("failed to add official runnable to controller-runtime manager: %w", err)
	}

	return nil
}
