package main

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/nais/elector/pkg/election/candidate"
	"github.com/nais/elector/pkg/election/official"
	"github.com/nais/elector/pkg/logging"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"os/signal"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"strings"
	"syscall"
	"time"

	"github.com/nais/liberator/pkg/logrus2logr"

	electormetrics "github.com/nais/elector/pkg/metrics"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var scheme = runtime.NewScheme()

const (
	ExitOK = iota
	ExitConfig
	ExitManagerCreation
	ExitManagerHealth
	ExitCandidateAdded
	ExitOfficialAdded
	ExitRuntime
)

// Configuration options
const (
	LogFormat         = "log-format"
	LogLevel          = "log-level"
	MetricsAddress    = "metrics-address"
	ProbeAddress      = "probe-address"
	ElectionAddress   = "http"
	ElectionName      = "election"
	ElectionNamespace = "election-namespace"
)

const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)

func init() {

	// Automatically read configuration options from environment variables.
	// i.e. --metrics-address will be configurable using ELECTOR_METRICS_ADDRESS.
	viper.SetEnvPrefix("elector")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))

	flag.String(MetricsAddress, "0.0.0.0:29090", "The address the metric endpoint binds to.")
	flag.String(ProbeAddress, "0.0.0.0:28080", "The address the probe endpoints binds to.")
	flag.String(ElectionAddress, "0.0.0.0:27070", "The address the election endpoints binds to.")
	flag.String(ElectionName, "", "The election name to take part in.")
	flag.String(ElectionNamespace, "", "The namespace the election is run in.")
	flag.String(LogFormat, "text", "Log format, either \"text\" or \"json\"")
	flag.String(LogLevel, "info", logLevelHelp())

	flag.Parse()

	err := viper.BindPFlags(flag.CommandLine)
	if err != nil {
		panic(err)
	}
}

func logLevelHelp() string {
	help := strings.Builder{}
	help.WriteString("Log level, one of: ")
	notFirst := false
	for _, level := range log.AllLevels {
		if notFirst {
			help.WriteString(", ")
		}
		help.WriteString(fmt.Sprintf("\"%s\"", level.String()))
		notFirst = true
	}
	return help.String()
}

func formatter(logFormat string) (log.Formatter, error) {
	switch logFormat {
	case LogFormatJSON:
		return &log.JSONFormatter{
			TimestampFormat:   time.RFC3339Nano,
			DisableHTMLEscape: true,
		}, nil
	case LogFormatText:
		return &log.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339Nano,
		}, nil
	}
	return nil, fmt.Errorf("unsupported log format '%s'", logFormat)
}

func main() {
	logger := configureLogging()

	electionName := types.NamespacedName{
		Namespace: viper.GetString(ElectionNamespace),
		Name:      viper.GetString(ElectionName),
	}

	if electionName.Name == "" || electionName.Namespace == "" {
		logger.Error(fmt.Errorf("both --election and --election-namespace are required options (sic)"))
		os.Exit(ExitConfig)
	}

	logger = logger.WithFields(log.Fields{
		"election_name": electionName.String(),
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				electionName.Namespace: {},
			},
		},
		Metrics: server.Options{
			BindAddress: viper.GetString(MetricsAddress),
		},
		HealthProbeBindAddress: viper.GetString(ProbeAddress),
		Logger:                 logr.New(&logrus2logr.Logrus2Logr{Logger: logger.WithField(logging.FieldComponent, "Controller")}),
	})
	if err != nil {
		logger.Error(fmt.Errorf("failed to start controller-runtime manager: %w", err))
		os.Exit(ExitManagerCreation)
	}
	err = mgr.AddHealthzCheck("manager", healthz.Ping)
	if err != nil {
		logger.Error(fmt.Errorf("failed to add default liveness: %w", err))
		os.Exit(ExitManagerHealth)
	}

	logger.Info("elector starting")
	terminator := context.Background()
	electionResults := make(chan string)

	err = candidate.AddCandidateToManager(mgr, logger, electionResults, electionName)
	if err != nil {
		logger.Error(err)
		os.Exit(ExitCandidateAdded)
	}

	err = official.AddOfficialToManager(mgr, logger, electionResults, viper.GetString(ElectionAddress))
	if err != nil {
		logger.Error(fmt.Errorf("failed to add election official to controller-runtime manager: %w", err))
		os.Exit(ExitOfficialAdded)
	}

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

		for {
			select {
			case sig := <-signals:
				logger.Infof("exiting due to signal: %s", strings.ToUpper(sig.String()))
				os.Exit(ExitOK)
			}
		}
	}()

	logger.Infof("starting manager")
	if err := mgr.Start(terminator); err != nil {
		logger.Error(fmt.Errorf("manager stopped unexpectedly: %s", err))
		os.Exit(ExitRuntime)
	}

	logger.Error(fmt.Errorf("manager has stopped"))
}

func configureLogging() log.FieldLogger {
	logger := log.New()
	logfmt, err := formatter(viper.GetString(LogFormat))
	if err != nil {
		logger.Error(fmt.Errorf("unable to configure log formatter: %w", err))
		os.Exit(ExitConfig)
	}

	logger.SetFormatter(logfmt)
	level, err := log.ParseLevel(viper.GetString(LogLevel))
	if err != nil {
		logger.Error(fmt.Errorf("unable to parse loglevel: %w", err))
		os.Exit(ExitConfig)
	}
	logger.SetLevel(level)

	logger.Infof("Logging configured")
	return logger
}

func init() {
	err := clientgoscheme.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}

	electormetrics.Register(metrics.Registry)
	// +kubebuilder:scaffold:scheme
}
