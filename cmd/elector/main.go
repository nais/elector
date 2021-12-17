package main

import (
	"context"
	"fmt"
	"github.com/nais/elector/pkg/election"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	ExitController
	ExitConfig
	ExitRuntime
)

// Configuration options
const (
	LogFormat         = "log-format"
	LogLevel          = "log-level"
	MetricsAddress    = "metrics-address"
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
	// i.e. --metrics-address will be configurable using ELECTOR_AIVEN_TOKEN.
	viper.SetEnvPrefix("elector")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))

	flag.String(MetricsAddress, "127.0.0.1:8080", "The address the metric endpoint binds to.")
	flag.String(ElectionAddress, "127.0.0.1:6060", "The address the election endpoints binds to.")
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

	electionName := types.NamespacedName{
		Namespace: viper.GetString(ElectionNamespace),
		Name:      viper.GetString(ElectionName),
	}

	if electionName.Name == "" || electionName.Namespace == "" {
		logger.Error(fmt.Errorf("both --election and --election-namespace are required options (sic)"))
		os.Exit(ExitConfig)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: viper.GetString(MetricsAddress),
	})
	if err != nil {
		logger.Error(fmt.Errorf("failed to start controller-runtime manager: %w", err))
		os.Exit(ExitController)
	}

	logger.Info("elector running")
	terminator := context.Background()
	electionResults := make(chan string)

	candidate := election.Candidate{
		Clock:           &clock.RealClock{},
		Logger:          logger,
		ElectionResults: electionResults,
		ElectionName:    electionName,
	}

	err = mgr.Add(&candidate)
	if err != nil {
		logger.Error(fmt.Errorf("failed to add candidate to controller-runtime manager: %w", err))
		os.Exit(ExitController)
	}

	electionManager := election.Manager{
		Logger:          logger,
		ElectionResults: electionResults,
		ElectionAddress: viper.GetString(ElectionAddress),
	}

	err = mgr.Add(&electionManager)
	if err != nil {
		logger.Error(fmt.Errorf("failed to add election manager to controller-runtime manager: %w", err))
		os.Exit(ExitController)
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

	if err := mgr.Start(terminator); err != nil {
		logger.Error(fmt.Errorf("manager stopped unexpectedly: %s", err))
		os.Exit(ExitRuntime)
	}

	logger.Error(fmt.Errorf("manager has stopped"))
}

func init() {
	err := clientgoscheme.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}

	electormetrics.Register(metrics.Registry)
	// +kubebuilder:scaffold:scheme
}
