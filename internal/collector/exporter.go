package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elohmeier/commvault-exporter/internal/commvault"
	"github.com/elohmeier/commvault-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const namespace = "commvault"

var moduleNames = []string{"vm", "dashboard", "jobs", "alerts", "events", "storage", "licensing"}

var coreModuleNames = map[string]bool{
	"vm": true, "dashboard": true, "jobs": true, "alerts": true, "events": true,
}

const initialRetryDelay = 15 * time.Second

type Exporter struct {
	cfg       config.Config
	client    *commvault.Client
	logger    *slog.Logger
	labelKeys []string

	refreshMu           sync.Mutex
	cacheMu             sync.Mutex
	startOnce           sync.Once
	schedulerCancel     context.CancelFunc
	moduleStates        map[string]*moduleState
	moduleSnapshots     map[string]moduleSnapshot
	dataTemplate        *dataMetrics
	hasRefresh          bool
	lastCompleteSuccess time.Time
	consecutiveErrs     int
	nextAttempt         time.Time
	now                 func() time.Time
	jitter              func(time.Duration) time.Duration
	retryBase           time.Duration

	up                   *prometheus.GaugeVec
	refreshDuration      *prometheus.GaugeVec
	refreshTotal         *prometheus.CounterVec
	refreshErrorsTotal   *prometheus.CounterVec
	collectorUp          *prometheus.GaugeVec
	subcollectorUp       *prometheus.GaugeVec
	cacheLastAttempt     *prometheus.GaugeVec
	cacheLastSuccess     *prometheus.GaugeVec
	cacheAge             *prometheus.GaugeVec
	cacheStale           *prometheus.GaugeVec
	refreshConsecutive   *prometheus.GaugeVec
	refreshInProgress    *prometheus.GaugeVec
	refreshNextAttempt   *prometheus.GaugeVec
	collectorDuration    *prometheus.GaugeVec
	collectorLastSuccess *prometheus.GaugeVec
	collectorCacheAge    *prometheus.GaugeVec
	collectorCacheStale  *prometheus.GaugeVec

	vmInfo              *prometheus.GaugeVec
	vmStatus            *prometheus.GaugeVec
	vmStatusCount       *prometheus.GaugeVec
	vmSize              *prometheus.GaugeVec
	vmUsed              *prometheus.GaugeVec
	vmGuest             *prometheus.GaugeVec
	vmBackupStart       *prometheus.GaugeVec
	vmBackupEnd         *prometheus.GaugeVec
	commcellInfo        *prometheus.GaugeVec
	slaStatusCount      *prometheus.GaugeVec
	jobs24hCount        *prometheus.GaugeVec
	healthStatusCount   *prometheus.GaugeVec
	entityCount         *prometheus.GaugeVec
	jobInfo             *prometheus.GaugeVec
	jobStatus           *prometheus.GaugeVec
	jobPercentComplete  *prometheus.GaugeVec
	jobElapsed          *prometheus.GaugeVec
	jobStart            *prometheus.GaugeVec
	jobLastUpdate       *prometheus.GaugeVec
	jobSizeApplication  *prometheus.GaugeVec
	jobFailedFiles      *prometheus.GaugeVec
	alertConfigInfo     *prometheus.GaugeVec
	alertTriggeredInfo  *prometheus.GaugeVec
	alertTriggeredTime  *prometheus.GaugeVec
	alertTriggeredCount *prometheus.GaugeVec
	alertUnreadCount    *prometheus.GaugeVec
	storageAccessEvents *prometheus.GaugeVec
	storageAccessLast   *prometheus.GaugeVec
	storageAccessTries  *prometheus.GaugeVec
	storagePoolInfo     *prometheus.GaugeVec
	storagePoolCapacity *prometheus.GaugeVec
	storagePoolFree     *prometheus.GaugeVec
	storagePolicyInfo   *prometheus.GaugeVec
	storagePolicyStream *prometheus.GaugeVec
	mediaAgentInfo      *prometheus.GaugeVec
	capacityUsage       *prometheus.GaugeVec
	capacityExpiry      *prometheus.GaugeVec
	licenseInfo         *prometheus.GaugeVec
	licenseAmount       *prometheus.GaugeVec
	librarySpace        *prometheus.GaugeVec
	libraryFreeRatio    *prometheus.GaugeVec
	libraryInfo         *prometheus.GaugeVec
	libraryReady        *prometheus.GaugeVec
	libraryMountPaths   *prometheus.GaugeVec
	mountPathInfo       *prometheus.GaugeVec
	mountPathReady      *prometheus.GaugeVec
	mountPathWriteOff   *prometheus.GaugeVec
	mountPathLogCaching *prometheus.GaugeVec

	commcellLicenseExpiry *prometheus.GaugeVec
	licenseExpiry         *prometheus.GaugeVec
}

type moduleState struct {
	Module       string        `json:"module"`
	LastAttempt  time.Time     `json:"-"`
	LastSuccess  time.Time     `json:"-"`
	LastDuration time.Duration `json:"-"`
	Attempts     uint64        `json:"attempts"`
	Errors       uint64        `json:"errors"`
	LastError    string        `json:"last_error,omitempty"`
}

type moduleSnapshot struct {
	Collectors []prometheus.Collector
	Published  time.Time
}

type cacheStatus struct {
	Ready             bool                `json:"ready"`
	LastAttemptUnix   int64               `json:"last_attempt_unix,omitempty"`
	LastSuccessUnix   int64               `json:"last_success_unix,omitempty"`
	AgeSeconds        float64             `json:"age_seconds"`
	MaxStaleSeconds   float64             `json:"max_stale_seconds"`
	Stale             bool                `json:"stale"`
	ConsecutiveErrors int                 `json:"consecutive_errors"`
	NextAttemptUnix   int64               `json:"next_attempt_unix,omitempty"`
	Modules           []cacheModuleStatus `json:"modules"`
}

type cacheModuleStatus struct {
	Module              string  `json:"module"`
	LastAttemptUnix     int64   `json:"last_attempt_unix,omitempty"`
	LastSuccessUnix     int64   `json:"last_success_unix,omitempty"`
	LastDurationSeconds float64 `json:"last_duration_seconds"`
	AgeSeconds          float64 `json:"age_seconds"`
	Stale               bool    `json:"stale"`
	Attempts            uint64  `json:"attempts"`
	Errors              uint64  `json:"errors"`
	LastError           string  `json:"last_error,omitempty"`
	Required            bool    `json:"required"`
	Published           bool    `json:"published"`
}

func New(cfg config.Config, client *commvault.Client, logger *slog.Logger) *Exporter {
	defaults := config.Default()
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaults.RefreshInterval
	}
	if cfg.RefreshTimeout <= 0 {
		cfg.RefreshTimeout = defaults.RefreshTimeout
	}
	if cfg.MaxStale <= 0 {
		cfg.MaxStale = defaults.MaxStale
	}
	if cfg.EventLookback <= 0 {
		cfg.EventLookback = defaults.EventLookback
	}
	if cfg.PageSize <= 0 {
		cfg.PageSize = defaults.PageSize
	}
	if cfg.JobCompletedLookupTime <= 0 {
		cfg.JobCompletedLookupTime = defaults.JobCompletedLookupTime
	}
	g := func(name, help string, labels []string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help}, withBaseLabels(cfg, labels))
	}
	c := func(name, help string, labels []string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: name, Help: help}, withBaseLabels(cfg, labels))
	}
	e := &Exporter{
		cfg:             cfg,
		client:          client,
		logger:          logger,
		labelKeys:       cfg.LabelKeys(),
		moduleStates:    make(map[string]*moduleState),
		moduleSnapshots: make(map[string]moduleSnapshot),
		now:             time.Now,
		retryBase:       initialRetryDelay,
		jitter: func(delay time.Duration) time.Duration {
			return time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
		},

		up:                   g("up", "Whether the last Commvault background refresh completed without collector errors.", nil),
		refreshDuration:      g("refresh_duration_seconds", "Duration of the last Commvault background refresh.", nil),
		refreshTotal:         c("refresh_total", "Total Commvault background refresh attempts.", nil),
		refreshErrorsTotal:   c("refresh_errors_total", "Total Commvault background refresh failures.", nil),
		collectorUp:          g("collector_up", "Whether the named Commvault collector completed successfully during the last refresh.", []string{"collector"}),
		subcollectorUp:       g("subcollector_up", "Whether the named Commvault subcollector completed successfully during the last refresh.", []string{"collector", "subcollector"}),
		cacheLastAttempt:     g("cache_last_attempt_timestamp_seconds", "Unix timestamp of the last Commvault cache refresh attempt.", nil),
		cacheLastSuccess:     g("cache_last_success_timestamp_seconds", "Unix timestamp of the oldest successful required collector snapshot.", nil),
		cacheAge:             g("cache_age_seconds", "Age of the oldest required collector snapshot currently served.", nil),
		cacheStale:           g("cache_stale", "1 when a required collector snapshot is stale or missing.", nil),
		refreshConsecutive:   g("refresh_consecutive_errors", "Number of consecutive background refresh cycles with collector errors.", nil),
		refreshInProgress:    g("refresh_in_progress", "1 while a background refresh cycle is running.", nil),
		refreshNextAttempt:   g("refresh_next_attempt_timestamp_seconds", "Unix timestamp of the next scheduled background refresh attempt; 0 while refreshing or stopped.", nil),
		collectorDuration:    g("collector_duration_seconds", "Duration of the latest collector attempt.", []string{"collector"}),
		collectorLastSuccess: g("collector_last_success_timestamp_seconds", "Unix timestamp of the latest successful collector snapshot.", []string{"collector"}),
		collectorCacheAge:    g("collector_cache_age_seconds", "Age of the latest successful collector snapshot.", []string{"collector"}),
		collectorCacheStale:  g("collector_cache_stale", "1 when the collector snapshot is stale or missing.", []string{"collector"}),
	}
	data := newDataMetrics(cfg)
	e.dataTemplate = data
	e.useDataMetrics(data)
	e.up.With(e.baseLabels()).Set(0)
	e.cacheStale.With(e.baseLabels()).Set(1)
	e.cacheAge.With(e.baseLabels()).Set(0)
	e.cacheLastAttempt.With(e.baseLabels()).Set(0)
	e.cacheLastSuccess.With(e.baseLabels()).Set(0)
	e.refreshDuration.With(e.baseLabels()).Set(0)
	e.refreshConsecutive.With(e.baseLabels()).Set(0)
	e.refreshInProgress.With(e.baseLabels()).Set(0)
	e.refreshNextAttempt.With(e.baseLabels()).Set(0)
	return e
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range append(e.statusCollectors(), e.dataTemplate.allCollectors()...) {
		collector.Describe(ch)
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.cacheMu.Lock()
	now := e.now()
	e.updateDynamicCacheMetricsLocked(now)
	collectors := e.statusCollectors()
	for _, module := range moduleNames {
		snapshot, ok := e.moduleSnapshots[module]
		if ok && now.Sub(snapshot.Published) <= e.cfg.MaxStale {
			collectors = append(collectors, snapshot.Collectors...)
		}
	}
	e.cacheMu.Unlock()
	for _, collector := range collectors {
		collector.Collect(ch)
	}
}

func (e *Exporter) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	e.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		e.schedulerCancel = cancel
		go e.schedulerLoop(runCtx)
	})
}

func (e *Exporter) Stop() {
	if e.schedulerCancel != nil {
		e.schedulerCancel()
	}
}

func (e *Exporter) RefreshOnce(ctx context.Context) error {
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if e.cfg.RefreshTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.cfg.RefreshTimeout)
		defer cancel()
	}
	start := e.now()
	e.cacheMu.Lock()
	e.cacheLastAttempt.With(e.baseLabels()).Set(float64(start.Unix()))
	e.refreshTotal.With(e.baseLabels()).Inc()
	e.refreshInProgress.With(e.baseLabels()).Set(1)
	e.refreshNextAttempt.With(e.baseLabels()).Set(0)
	e.cacheMu.Unlock()

	staging := newDataMetrics(e.cfg)
	e.useDataMetrics(staging)
	type moduleSpec struct {
		name string
		fn   func(context.Context) error
	}
	specs := []moduleSpec{
		{name: "vm", fn: e.collectVMs},
		{name: "dashboard", fn: e.collectDashboard},
		{name: "jobs", fn: e.collectJobs},
		{name: "alerts", fn: e.collectAlerts},
		{name: "events", fn: e.collectEvents},
		{name: "storage", fn: e.collectStorage},
		{name: "licensing", fn: e.collectLicensing},
	}
	results := make(chan bool, len(specs))
	var wg sync.WaitGroup
	enabled := 0
	for _, spec := range specs {
		if e.cfg.IsModuleDisabled(spec.name) {
			continue
		}
		enabled++
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- e.runModule(ctx, spec.name, spec.fn, staging.moduleCollectors(spec.name))
		}()
	}
	wg.Wait()
	close(results)
	failed := false
	for result := range results {
		failed = failed || !result
	}
	duration := e.now().Sub(start)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.hasRefresh = true
	e.refreshDuration.With(e.baseLabels()).Set(duration.Seconds())
	e.refreshInProgress.With(e.baseLabels()).Set(0)
	if enabled == 0 {
		failed = false
	}
	if failed {
		e.up.With(e.baseLabels()).Set(0)
		e.refreshErrorsTotal.With(e.baseLabels()).Inc()
		e.consecutiveErrs++
		e.refreshConsecutive.With(e.baseLabels()).Set(float64(e.consecutiveErrs))
		return fmt.Errorf("one or more collectors failed")
	}
	e.up.With(e.baseLabels()).Set(1)
	e.consecutiveErrs = 0
	e.lastCompleteSuccess = e.now()
	e.refreshConsecutive.With(e.baseLabels()).Set(0)
	e.updateDynamicCacheMetricsLocked(e.now())
	return nil
}

func (e *Exporter) ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	status := e.cacheStatus(e.now())
	w.Header().Set("Content-Type", "application/json")
	if !status.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (e *Exporter) DebugCacheHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e.cacheStatus(e.now()))
}

func (e *Exporter) schedulerLoop(ctx context.Context) {
	delay := time.Duration(0)
	for {
		if delay > 0 {
			e.setNextAttempt(e.now().Add(delay))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				e.setNextAttempt(time.Time{})
				return
			case <-timer.C:
			}
		}
		e.setNextAttempt(time.Time{})
		err := e.RefreshOnce(ctx)
		if ctx.Err() != nil {
			e.setNextAttempt(time.Time{})
			return
		}
		if err != nil {
			if e.logger != nil {
				e.logger.Error("refresh failed", "err", err)
			}
			delay = e.retryDelay()
			continue
		}
		delay = e.cfg.RefreshInterval
	}
}

func (e *Exporter) retryDelay() time.Duration {
	e.cacheMu.Lock()
	consecutive := e.consecutiveErrs
	e.cacheMu.Unlock()
	delay := e.retryBase
	for i := 1; i < consecutive && delay < e.cfg.RefreshInterval; i++ {
		if delay >= e.cfg.RefreshInterval/2 {
			delay = e.cfg.RefreshInterval
			break
		}
		delay *= 2
	}
	if delay > e.cfg.RefreshInterval {
		delay = e.cfg.RefreshInterval
	}
	delay = e.jitter(delay)
	if delay > e.cfg.RefreshInterval {
		return e.cfg.RefreshInterval
	}
	return delay
}

func (e *Exporter) setNextAttempt(next time.Time) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.nextAttempt = next
	value := float64(0)
	if !next.IsZero() {
		value = float64(next.Unix())
	}
	e.refreshNextAttempt.With(e.baseLabels()).Set(value)
}

func (e *Exporter) runModule(ctx context.Context, name string, fn func(context.Context) error, collectors []prometheus.Collector) bool {
	start := e.now()
	e.cacheMu.Lock()
	state := e.moduleStateLocked(name)
	state.Attempts++
	state.LastAttempt = start
	e.cacheMu.Unlock()
	err := fn(ctx)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	state.LastDuration = e.now().Sub(start)
	e.collectorDuration.With(e.baseLabels("collector", name)).Set(state.LastDuration.Seconds())
	if err != nil {
		state.Errors++
		state.LastError = err.Error()
		e.collectorUp.With(e.baseLabels("collector", name)).Set(0)
		if e.logger != nil {
			e.logger.Error("collector failed", "collector", name, "err", err)
		}
		return false
	}
	state.LastSuccess = e.now()
	state.LastError = ""
	e.collectorUp.With(e.baseLabels("collector", name)).Set(1)
	e.collectorLastSuccess.With(e.baseLabels("collector", name)).Set(float64(state.LastSuccess.Unix()))
	e.moduleSnapshots[name] = moduleSnapshot{Collectors: collectors, Published: state.LastSuccess}
	return true
}

func (e *Exporter) runSubcollector(ctx context.Context, collector, subcollector string, fn func(context.Context) error) error {
	err := fn(ctx)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if err != nil {
		e.subcollectorUp.With(e.baseLabels("collector", collector, "subcollector", subcollector)).Set(0)
		if e.logger != nil {
			e.logger.Error("subcollector failed", "collector", collector, "subcollector", subcollector, "err", err)
		}
		return err
	}
	e.subcollectorUp.With(e.baseLabels("collector", collector, "subcollector", subcollector)).Set(1)
	return nil
}

func (e *Exporter) moduleStateLocked(name string) *moduleState {
	if state, ok := e.moduleStates[name]; ok {
		return state
	}
	state := &moduleState{Module: name}
	e.moduleStates[name] = state
	return state
}

func (e *Exporter) cacheStatus(now time.Time) cacheStatus {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.updateDynamicCacheMetricsLocked(now)
	ready, lastSuccess, age, stale := e.readinessLocked(now)
	lastAttempt := gaugeValue(e.cacheLastAttempt.With(e.baseLabels()))
	required := e.requiredModules()
	modules := make([]cacheModuleStatus, 0, len(moduleNames))
	for _, name := range moduleNames {
		if e.cfg.IsModuleDisabled(name) {
			continue
		}
		state := e.moduleStateLocked(name)
		moduleAge := 0.0
		moduleStale := true
		if !state.LastSuccess.IsZero() {
			moduleAge = now.Sub(state.LastSuccess).Seconds()
			moduleStale = moduleAge > e.cfg.MaxStale.Seconds()
		}
		var attemptUnix, successUnix int64
		if !state.LastAttempt.IsZero() {
			attemptUnix = state.LastAttempt.Unix()
		}
		if !state.LastSuccess.IsZero() {
			successUnix = state.LastSuccess.Unix()
		}
		modules = append(modules, cacheModuleStatus{
			Module:              name,
			LastAttemptUnix:     attemptUnix,
			LastSuccessUnix:     successUnix,
			LastDurationSeconds: state.LastDuration.Seconds(),
			AgeSeconds:          moduleAge,
			Stale:               moduleStale,
			Attempts:            state.Attempts,
			Errors:              state.Errors,
			LastError:           state.LastError,
			Required:            required[name],
			Published:           !state.LastSuccess.IsZero(),
		})
	}
	nextAttemptUnix := int64(0)
	if !e.nextAttempt.IsZero() {
		nextAttemptUnix = e.nextAttempt.Unix()
	}
	lastSuccessUnix := int64(0)
	if !lastSuccess.IsZero() {
		lastSuccessUnix = lastSuccess.Unix()
	}
	return cacheStatus{
		Ready:             ready,
		LastAttemptUnix:   int64(lastAttempt),
		LastSuccessUnix:   lastSuccessUnix,
		AgeSeconds:        age,
		MaxStaleSeconds:   e.cfg.MaxStale.Seconds(),
		Stale:             stale,
		ConsecutiveErrors: e.consecutiveErrs,
		NextAttemptUnix:   nextAttemptUnix,
		Modules:           modules,
	}
}

func (e *Exporter) updateDynamicCacheMetricsLocked(now time.Time) {
	_, lastSuccess, age, stale := e.readinessLocked(now)
	lastSuccessValue := float64(0)
	if !lastSuccess.IsZero() {
		lastSuccessValue = float64(lastSuccess.Unix())
	}
	e.cacheLastSuccess.With(e.baseLabels()).Set(lastSuccessValue)
	e.cacheAge.With(e.baseLabels()).Set(age)
	e.cacheStale.With(e.baseLabels()).Set(boolFloat(stale))
	for _, name := range moduleNames {
		if e.cfg.IsModuleDisabled(name) {
			continue
		}
		snapshot, ok := e.moduleSnapshots[name]
		moduleAge := 0.0
		moduleStale := true
		if ok {
			moduleAge = now.Sub(snapshot.Published).Seconds()
			moduleStale = moduleAge > e.cfg.MaxStale.Seconds()
		}
		e.collectorCacheAge.With(e.baseLabels("collector", name)).Set(moduleAge)
		e.collectorCacheStale.With(e.baseLabels("collector", name)).Set(boolFloat(moduleStale))
	}
}

func (e *Exporter) readinessLocked(now time.Time) (bool, time.Time, float64, bool) {
	required := e.requiredModules()
	if len(required) == 0 {
		if !e.hasRefresh || e.lastCompleteSuccess.IsZero() {
			return false, time.Time{}, 0, true
		}
		age := now.Sub(e.lastCompleteSuccess).Seconds()
		return age <= e.cfg.MaxStale.Seconds(), e.lastCompleteSuccess, age, age > e.cfg.MaxStale.Seconds()
	}
	var oldest time.Time
	for name := range required {
		snapshot, ok := e.moduleSnapshots[name]
		if !ok {
			return false, time.Time{}, 0, true
		}
		if oldest.IsZero() || snapshot.Published.Before(oldest) {
			oldest = snapshot.Published
		}
	}
	age := now.Sub(oldest).Seconds()
	stale := age > e.cfg.MaxStale.Seconds()
	return !stale, oldest, age, stale
}

func (e *Exporter) requiredModules() map[string]bool {
	required := make(map[string]bool)
	for _, name := range moduleNames {
		if !e.cfg.IsModuleDisabled(name) && coreModuleNames[name] {
			required[name] = true
		}
	}
	if len(required) > 0 {
		return required
	}
	for _, name := range moduleNames {
		if !e.cfg.IsModuleDisabled(name) {
			required[name] = true
		}
	}
	return required
}

func (e *Exporter) statusCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		e.up, e.refreshDuration, e.refreshTotal, e.refreshErrorsTotal, e.collectorUp, e.subcollectorUp,
		e.cacheLastAttempt, e.cacheLastSuccess, e.cacheAge, e.cacheStale,
		e.refreshConsecutive, e.refreshInProgress, e.refreshNextAttempt,
		e.collectorDuration, e.collectorLastSuccess, e.collectorCacheAge, e.collectorCacheStale,
	}
}

func (e *Exporter) baseLabels(extra ...string) prometheus.Labels {
	labels := prometheus.Labels{}
	for _, key := range e.labelKeys {
		labels[key] = e.cfg.Labels[key]
	}
	if len(extra)%2 == 0 {
		for i := 0; i < len(extra); i += 2 {
			labels[extra[i]] = extra[i+1]
		}
	}
	return labels
}

func withBaseLabels(cfg config.Config, labels []string) []string {
	out := cfg.LabelKeys()
	return append(out, labels...)
}

func gaugeValue(metric prometheus.Gauge) float64 {
	m := &dto.Metric{}
	if err := metric.Write(m); err == nil && m.Gauge != nil && m.Gauge.Value != nil {
		return *m.Gauge.Value
	}
	return 0
}

func statusName(code int) string {
	switch code {
	case 0:
		return "all"
	case 1:
		return "protected"
	case 2:
		return "not_protected"
	case 3:
		return "pending"
	case 4:
		return "backed_up_with_error"
	case 5:
		return "discovered"
	case 7:
		return "backed_up_with_warning"
	case 8:
		return "deleted"
	default:
		return fmt.Sprintf("status_%d", code)
	}
}

func boolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

func f(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		out, _ := n.Float64()
		return out
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		out, _ := strconv.ParseFloat(n, 64)
		return out
	default:
		return 0
	}
}

func s(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func tableRows(resp commvault.TabularResponse) []map[string]any {
	rows := make([]map[string]any, 0, len(resp.Records))
	for _, record := range resp.Records {
		row := map[string]any{}
		for i, value := range record {
			if i >= len(resp.Columns) {
				continue
			}
			key := resp.Columns[i].Name
			if key == "" {
				key = resp.Columns[i].Data
			}
			row[key] = value
		}
		rows = append(rows, row)
	}
	return rows
}

func joinPlans(plans []struct {
	Name string `json:"planName"`
	ID   int64  `json:"planId"`
}) string {
	values := make([]string, 0, len(plans))
	for _, plan := range plans {
		if plan.Name != "" {
			values = append(values, plan.Name)
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
