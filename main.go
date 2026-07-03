package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/elohmeier/commvault-exporter/internal/collector"
	"github.com/elohmeier/commvault-exporter/internal/commvault"
	"github.com/elohmeier/commvault-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	app     = "Commvault-Exporter"
	version = "dev"
	build   = "none"
)

var reservedCustomLabelNames = []string{
	"alert", "alert_id", "app", "backup_level", "category", "client", "collector", "commcell", "copies", "criterion",
	"deleted", "dial", "entity", "eval_expiry_date", "guid", "health_status", "job_id", "job_type", "kind",
	"library", "library_id", "license", "license_id", "media_agent", "media_agent_id", "operation", "os", "plans",
	"policy", "policy_id", "pool", "pool_id",
	"proxy_client", "read", "release", "severity", "status", "status_name", "subclient", "type", "version",
	"report", "summary", "unit", "vm", "vsa_client",
}

var (
	exit                = os.Exit
	listenAndServe      = (*http.Server).ListenAndServe
	shutdownServer      = (*http.Server).Shutdown
	signalNotify        = signal.Notify
	signalStop          = signal.Stop
	newCommvaultClient  = commvault.NewClient
	newCommvaultMetrics = commvault.NewMetrics
	newExporter         = collector.New
)

func main() {
	exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	var (
		urlStr                 string
		authMode               string
		labelsStr              string
		disabledModulesStr     string
		caFile                 string
		bindPort               int
		pageSize               int
		jobCompletedLookupTime int
		timeout                time.Duration
		refreshInterval        time.Duration
		refreshTimeout         time.Duration
		maxStale               time.Duration
		ignoreCert             bool
		showVersion            bool
		debug                  bool
	)
	flags := flag.NewFlagSet(app, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&urlStr, "url", "", "Commvault base URL (e.g., https://commvault.example.com)")
	flags.StringVar(&authMode, "auth-mode", "", "Authentication header mode: authtoken or bearer (default: authtoken)")
	flags.StringVar(&labelsStr, "labels", "", "Custom labels in key=value format, comma-separated")
	flags.StringVar(&disabledModulesStr, "disabled-modules", "", "Comma-separated list of collectors to disable")
	flags.IntVar(&bindPort, "bind-port", 9720, "Port to bind the exporter endpoint to")
	flags.IntVar(&pageSize, "page-size", 0, "Commvault API page size (default: 1000)")
	flags.IntVar(&jobCompletedLookupTime, "job-completed-lookup-time", 0, "Job completed lookup window in seconds (default: 86400)")
	flags.DurationVar(&timeout, "timeout", 0, "Commvault request timeout (default: 30s)")
	flags.DurationVar(&refreshInterval, "refresh-interval", 0, "Background cache refresh interval (default: 5m)")
	flags.DurationVar(&refreshTimeout, "refresh-timeout", 0, "Background cache refresh timeout (default: 2m)")
	flags.DurationVar(&maxStale, "max-stale", 0, "Maximum cache age before readiness fails (default: 15m)")
	flags.BoolVar(&ignoreCert, "ignore-cert", false, "Disable TLS certificate verification")
	flags.StringVar(&caFile, "ca-file", "", "Path to a custom CA certificate bundle")
	flags.BoolVar(&showVersion, "version", false, "Display application version")
	flags.BoolVar(&debug, "debug", false, "Enable debug logging")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if showVersion {
		_, _ = fmt.Fprintf(stdout, "%s v%s build %s\n", app, version, build)
		return 0
	}
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	})).With("app", app, "version", "v"+version, "build", build)

	if urlStr == "" {
		urlStr = config.GetURL()
	}
	if urlStr == "" {
		logger.Error("URL is required (use -url flag or COMMVAULT_URL env var)")
		flags.Usage()
		return 1
	}
	username, password := config.GetCredentials()
	authToken := config.GetAuthToken()
	if authToken == "" && (username == "" || password == "") {
		logger.Error("credentials are required via COMMVAULT_USERNAME and COMMVAULT_PASSWORD, or set COMMVAULT_AUTH_TOKEN")
		return 1
	}

	cfg := config.Default()
	cfg.Paths = config.ApplyPathEnv(cfg.Paths)
	cfg.Labels = config.ParseLabels(config.GetLabels())
	for key, value := range config.ParseLabels(labelsStr) {
		cfg.Labels[key] = value
	}
	if err := config.ValidateLabels(cfg.Labels, reservedCustomLabelNames); err != nil {
		logger.Error("invalid label configuration", "err", err)
		return 1
	}
	cfg.DisabledModules = append(config.ParseCSV(config.GetDisabledModules()), config.ParseCSV(disabledModulesStr)...)
	if authMode == "" {
		authMode = config.GetAuthMode()
	}
	if authMode != "" {
		cfg.AuthMode = authMode
	}
	var err error
	cfg.PageSize, err = config.ChooseInt(pageSize, config.GetPageSize(), cfg.PageSize, "page-size")
	if err != nil {
		logger.Error("invalid page size", "err", err)
		return 1
	}
	cfg.JobCompletedLookupTime, err = config.ChooseInt(jobCompletedLookupTime, config.GetJobCompletedLookupTime(), cfg.JobCompletedLookupTime, "job-completed-lookup-time")
	if err != nil {
		logger.Error("invalid job completed lookup time", "err", err)
		return 1
	}
	cfg.Timeout, err = config.ChooseDuration(timeout, config.GetTimeout(), cfg.Timeout, "timeout")
	if err != nil {
		logger.Error("invalid timeout", "err", err)
		return 1
	}
	cfg.RefreshInterval, err = config.ChooseDuration(refreshInterval, config.GetRefreshInterval(), cfg.RefreshInterval, "refresh-interval")
	if err != nil {
		logger.Error("invalid refresh interval", "err", err)
		return 1
	}
	cfg.RefreshTimeout, err = config.ChooseDuration(refreshTimeout, config.GetRefreshTimeout(), cfg.RefreshTimeout, "refresh-timeout")
	if err != nil {
		logger.Error("invalid refresh timeout", "err", err)
		return 1
	}
	cfg.MaxStale, err = config.ChooseDuration(maxStale, config.GetMaxStale(), cfg.MaxStale, "max-stale")
	if err != nil {
		logger.Error("invalid max stale", "err", err)
		return 1
	}
	if caFile == "" {
		caFile = config.GetCAFile()
	}
	ignoreCert = ignoreCert || config.GetIgnoreCert()
	if ignoreCert {
		logger.Info("TLS certificate verification disabled")
	}
	if caFile != "" {
		logger.Info("using custom CA file", "path", caFile)
	}

	apiMetrics := newCommvaultMetrics("commvault")
	client, err := newCommvaultClient(commvault.Config{
		BaseURL:            urlStr,
		Username:           username,
		Password:           password,
		AuthToken:          authToken,
		AuthMode:           cfg.AuthMode,
		Timeout:            cfg.Timeout,
		PageSize:           cfg.PageSize,
		InsecureSkipVerify: ignoreCert,
		CAFile:             caFile,
		UserAgent:          fmt.Sprintf("commvault-exporter/%s", version),
		Metrics:            apiMetrics,
	})
	if err != nil {
		logger.Error("failed to create Commvault client", "err", err)
		return 1
	}

	registry := prometheus.NewRegistry()
	registerer := prometheus.Registerer(registry)
	exporter := newExporter(cfg, client, logger)
	registerer.MustRegister(apiMetrics.Collectors()...)
	registerer.MustRegister(exporter)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	exporter.Start(appCtx)
	defer exporter.Stop()

	listenAddr := ":" + strconv.Itoa(bindPort)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newMux(registry, exporter),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", listenAddr, "url", urlStr, "disabled_modules", len(cfg.DisabledModules))
		errCh <- listenAndServe(server)
	}()
	sigCh := make(chan os.Signal, 1)
	signalNotify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signalStop(sigCh)
	select {
	case sig := <-sigCh:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	appCancel()
	exporter.Stop()
	if err := shutdownServer(server, ctx); err != nil {
		logger.Error("server shutdown failed", "err", err)
		return 1
	}
	return 0
}

func newMux(registry *prometheus.Registry, exporter *collector.Exporter) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/readyz", exporter.ReadyHandler)
	mux.HandleFunc("/debug/cache", exporter.DebugCacheHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(app + " - /metrics for Prometheus metrics"))
	})
	return mux
}
