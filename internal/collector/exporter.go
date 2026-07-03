package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

type Exporter struct {
	cfg       config.Config
	client    *commvault.Client
	logger    *slog.Logger
	labelKeys []string

	refreshMu       sync.Mutex
	cacheMu         sync.Mutex
	startOnce       sync.Once
	schedulerCancel context.CancelFunc
	moduleStates    map[string]*moduleState

	up                 *prometheus.GaugeVec
	refreshDuration    *prometheus.GaugeVec
	refreshTotal       *prometheus.CounterVec
	refreshErrorsTotal *prometheus.CounterVec
	collectorUp        *prometheus.GaugeVec
	subcollectorUp     *prometheus.GaugeVec
	cacheLastAttempt   *prometheus.GaugeVec
	cacheLastSuccess   *prometheus.GaugeVec
	cacheAge           *prometheus.GaugeVec
	cacheStale         *prometheus.GaugeVec

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
	storagePoolInfo     *prometheus.GaugeVec
	storagePoolCapacity *prometheus.GaugeVec
	storagePoolFree     *prometheus.GaugeVec
	storagePolicyInfo   *prometheus.GaugeVec
	storagePolicyStream *prometheus.GaugeVec
	mediaAgentInfo      *prometheus.GaugeVec
	capacityUsage       *prometheus.GaugeVec
	licenseInfo         *prometheus.GaugeVec
	licenseAmount       *prometheus.GaugeVec
	librarySpace        *prometheus.GaugeVec
	libraryFreeRatio    *prometheus.GaugeVec
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

type cacheStatus struct {
	Ready           bool                `json:"ready"`
	LastAttemptUnix int64               `json:"last_attempt_unix,omitempty"`
	LastSuccessUnix int64               `json:"last_success_unix,omitempty"`
	AgeSeconds      float64             `json:"age_seconds"`
	MaxStaleSeconds float64             `json:"max_stale_seconds"`
	Stale           bool                `json:"stale"`
	Modules         []cacheModuleStatus `json:"modules"`
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
		cfg:          cfg,
		client:       client,
		logger:       logger,
		labelKeys:    cfg.LabelKeys(),
		moduleStates: make(map[string]*moduleState),

		up:                 g("up", "Whether the last Commvault background refresh completed without collector errors.", nil),
		refreshDuration:    g("refresh_duration_seconds", "Duration of the last Commvault background refresh.", nil),
		refreshTotal:       c("refresh_total", "Total Commvault background refresh attempts.", nil),
		refreshErrorsTotal: c("refresh_errors_total", "Total Commvault background refresh failures.", nil),
		collectorUp:        g("collector_up", "Whether the named Commvault collector completed successfully during the last refresh.", []string{"collector"}),
		subcollectorUp:     g("subcollector_up", "Whether the named Commvault subcollector completed successfully during the last refresh.", []string{"collector", "subcollector"}),
		cacheLastAttempt:   g("cache_last_attempt_timestamp_seconds", "Unix timestamp of the last Commvault cache refresh attempt.", nil),
		cacheLastSuccess:   g("cache_last_success_timestamp_seconds", "Unix timestamp of the last successful Commvault cache refresh.", nil),
		cacheAge:           g("cache_age_seconds", "Age of the cached Commvault data currently served.", nil),
		cacheStale:         g("cache_stale", "1 when the cached Commvault data is stale or missing.", nil),

		vmInfo:              g("vm_info", "Commvault VM inventory metadata.", []string{"vm", "guid", "client", "subclient", "proxy_client", "vsa_client", "os", "deleted"}),
		vmStatus:            g("vm_status", "Commvault VM backup status code.", []string{"vm", "guid", "status", "status_name"}),
		vmStatusCount:       g("vm_status_count", "Commvault VM count by backup status.", []string{"status", "status_name"}),
		vmSize:              g("vm_size_bytes", "Commvault VM allocated size in bytes.", []string{"vm", "guid"}),
		vmUsed:              g("vm_used_space_bytes", "Commvault VM used disk space in bytes.", []string{"vm", "guid"}),
		vmGuest:             g("vm_guest_space_bytes", "Commvault VM guest-used space in bytes.", []string{"vm", "guid"}),
		vmBackupStart:       g("vm_last_backup_start_time_seconds", "Commvault VM last backup start timestamp.", []string{"vm", "guid"}),
		vmBackupEnd:         g("vm_last_backup_end_time_seconds", "Commvault VM last backup end timestamp.", []string{"vm", "guid"}),
		commcellInfo:        g("commcell_info", "Commvault CommCell metadata.", []string{"commcell", "version", "release"}),
		slaStatusCount:      g("sla_status_count", "Commvault SLA status counts.", []string{"commcell", "status"}),
		jobs24hCount:        g("jobs_24h_count", "Commvault dashboard job counts for the current 24h window.", []string{"commcell", "status"}),
		healthStatusCount:   g("health_status_count", "Commvault health overview counts.", []string{"commcell", "status"}),
		entityCount:         g("environment_entity_count", "Commvault environment entity counts.", []string{"commcell", "entity"}),
		jobInfo:             g("job_info", "Commvault job metadata.", []string{"job_id", "job_type", "operation", "status", "client", "app", "subclient", "backup_level"}),
		jobStatus:           g("job_status", "Commvault job status as a one-hot gauge.", []string{"job_id", "status"}),
		jobPercentComplete:  g("job_percent_complete", "Commvault job percent complete.", []string{"job_id"}),
		jobElapsed:          g("job_elapsed_seconds", "Commvault job elapsed seconds.", []string{"job_id"}),
		jobStart:            g("job_start_time_seconds", "Commvault job start timestamp.", []string{"job_id"}),
		jobLastUpdate:       g("job_last_update_time_seconds", "Commvault job last update timestamp.", []string{"job_id"}),
		jobSizeApplication:  g("job_size_application_bytes", "Commvault job application size in bytes.", []string{"job_id"}),
		jobFailedFiles:      g("job_failed_files", "Commvault job failed file count.", []string{"job_id"}),
		alertConfigInfo:     g("alert_config_info", "Commvault configured alert metadata.", []string{"alert_id", "alert", "type", "category", "creator", "status"}),
		alertTriggeredInfo:  g("alert_triggered_info", "Commvault triggered alert metadata.", []string{"alert_id", "severity", "type", "criterion", "client", "job_id", "commcell", "read"}),
		alertTriggeredTime:  g("alert_triggered_detected_time_seconds", "Commvault triggered alert detected timestamp.", []string{"alert_id", "severity", "type", "criterion", "client", "job_id", "commcell", "read"}),
		alertTriggeredCount: g("alert_triggered_count", "Commvault triggered alert count by severity and type.", []string{"severity", "type"}),
		alertUnreadCount:    g("alert_unread_count", "Commvault unread triggered alert count.", nil),
		storagePoolInfo:     g("storage_pool_info", "Commvault storage pool metadata.", []string{"pool_id", "pool", "status", "type", "client_group"}),
		storagePoolCapacity: g("storage_pool_capacity_bytes", "Commvault storage pool total capacity in bytes.", []string{"pool_id", "pool"}),
		storagePoolFree:     g("storage_pool_free_bytes", "Commvault storage pool free bytes.", []string{"pool_id", "pool"}),
		storagePolicyInfo:   g("storage_policy_info", "Commvault storage policy metadata.", []string{"policy_id", "policy", "type", "copies", "plans"}),
		storagePolicyStream: g("storage_policy_streams", "Commvault storage policy stream count.", []string{"policy_id", "policy"}),
		mediaAgentInfo:      g("media_agent_info", "Commvault media agent metadata.", []string{"media_agent_id", "media_agent"}),
		capacityUsage:       g("capacity_usage", "Commvault capacity usage by dial.", []string{"dial", "kind"}),
		licenseInfo:         g("license_info", "Commvault license report metadata.", []string{"license_id", "license", "report", "unit", "summary", "eval_expiry_date"}),
		licenseAmount:       g("license_amount", "Commvault license amount by report, license, unit, and kind.", []string{"license_id", "license", "report", "unit", "kind"}),
		librarySpace:        g("library_space_bytes", "Commvault library space by kind.", []string{"library_id", "library", "health_status", "kind"}),
		libraryFreeRatio:    g("library_free_ratio", "Commvault library free-space ratio.", []string{"library_id", "library", "health_status"}),
	}
	e.up.With(e.baseLabels()).Set(0)
	e.cacheStale.With(e.baseLabels()).Set(1)
	e.cacheAge.With(e.baseLabels()).Set(0)
	e.cacheLastAttempt.With(e.baseLabels()).Set(0)
	e.cacheLastSuccess.With(e.baseLabels()).Set(0)
	e.refreshDuration.With(e.baseLabels()).Set(0)
	return e
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range e.allCollectors() {
		collector.Describe(ch)
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.cacheMu.Lock()
	e.updateDynamicCacheMetricsLocked(time.Now())
	collectors := e.allCollectors()
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
	start := time.Now()
	e.cacheMu.Lock()
	e.cacheLastAttempt.With(e.baseLabels()).Set(float64(start.Unix()))
	e.refreshTotal.With(e.baseLabels()).Inc()
	e.cacheMu.Unlock()

	e.resetDataMetrics()
	results := []bool{
		e.runModule(ctx, "vm", e.collectVMs),
		e.runModule(ctx, "dashboard", e.collectDashboard),
		e.runModule(ctx, "jobs", e.collectJobs),
		e.runModule(ctx, "alerts", e.collectAlerts),
		e.runModule(ctx, "storage", e.collectStorage),
		e.runModule(ctx, "licensing", e.collectLicensing),
	}
	failed := false
	for _, r := range results {
		if !r {
			failed = true
		}
	}
	duration := time.Since(start)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.refreshDuration.With(e.baseLabels()).Set(duration.Seconds())
	if failed {
		e.up.With(e.baseLabels()).Set(0)
		e.refreshErrorsTotal.With(e.baseLabels()).Inc()
		return fmt.Errorf("one or more collectors failed")
	}
	e.up.With(e.baseLabels()).Set(1)
	e.cacheLastSuccess.With(e.baseLabels()).Set(float64(time.Now().Unix()))
	return nil
}

func (e *Exporter) ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	status := e.cacheStatus(time.Now())
	if !status.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (e *Exporter) DebugCacheHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e.cacheStatus(time.Now()))
}

func (e *Exporter) schedulerLoop(ctx context.Context) {
	if err := e.RefreshOnce(ctx); err != nil && e.logger != nil {
		e.logger.Error("initial refresh failed", "err", err)
	}
	ticker := time.NewTicker(e.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.RefreshOnce(ctx); err != nil && e.logger != nil {
				e.logger.Error("refresh failed", "err", err)
			}
		}
	}
}

func (e *Exporter) runModule(ctx context.Context, name string, fn func(context.Context) error) bool {
	if e.cfg.IsModuleDisabled(name) {
		return true
	}
	start := time.Now()
	e.cacheMu.Lock()
	state := e.moduleStateLocked(name)
	state.Attempts++
	state.LastAttempt = start
	e.cacheMu.Unlock()
	err := fn(ctx)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	state.LastDuration = time.Since(start)
	if err != nil {
		state.Errors++
		state.LastError = err.Error()
		e.collectorUp.With(e.baseLabels("collector", name)).Set(0)
		if e.logger != nil {
			e.logger.Error("collector failed", "collector", name, "err", err)
		}
		return false
	}
	state.LastSuccess = time.Now()
	state.LastError = ""
	e.collectorUp.With(e.baseLabels("collector", name)).Set(1)
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
	lastSuccess := gaugeValue(e.cacheLastSuccess.With(e.baseLabels()))
	lastAttempt := gaugeValue(e.cacheLastAttempt.With(e.baseLabels()))
	age := 0.0
	stale := true
	if lastSuccess > 0 {
		age = now.Sub(time.Unix(int64(lastSuccess), 0)).Seconds()
		stale = age > e.cfg.MaxStale.Seconds()
	}
	modules := make([]cacheModuleStatus, 0, len(e.moduleStates))
	keys := make([]string, 0, len(e.moduleStates))
	for name := range e.moduleStates {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		state := e.moduleStates[name]
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
		})
	}
	return cacheStatus{
		Ready:           lastSuccess > 0 && !stale,
		LastAttemptUnix: int64(lastAttempt),
		LastSuccessUnix: int64(lastSuccess),
		AgeSeconds:      age,
		MaxStaleSeconds: e.cfg.MaxStale.Seconds(),
		Stale:           stale,
		Modules:         modules,
	}
}

func (e *Exporter) updateDynamicCacheMetricsLocked(now time.Time) {
	lastSuccess := gaugeValue(e.cacheLastSuccess.With(e.baseLabels()))
	age := 0.0
	stale := 1.0
	if lastSuccess > 0 {
		age = now.Sub(time.Unix(int64(lastSuccess), 0)).Seconds()
		if age <= e.cfg.MaxStale.Seconds() {
			stale = 0
		}
	}
	e.cacheAge.With(e.baseLabels()).Set(age)
	e.cacheStale.With(e.baseLabels()).Set(stale)
}

func (e *Exporter) allCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		e.up, e.refreshDuration, e.refreshTotal, e.refreshErrorsTotal, e.collectorUp, e.subcollectorUp, e.cacheLastAttempt, e.cacheLastSuccess, e.cacheAge, e.cacheStale,
		e.vmInfo, e.vmStatus, e.vmStatusCount, e.vmSize, e.vmUsed, e.vmGuest, e.vmBackupStart, e.vmBackupEnd,
		e.commcellInfo, e.slaStatusCount, e.jobs24hCount, e.healthStatusCount, e.entityCount,
		e.jobInfo, e.jobStatus, e.jobPercentComplete, e.jobElapsed, e.jobStart, e.jobLastUpdate, e.jobSizeApplication, e.jobFailedFiles,
		e.alertConfigInfo, e.alertTriggeredInfo, e.alertTriggeredTime, e.alertTriggeredCount, e.alertUnreadCount,
		e.storagePoolInfo, e.storagePoolCapacity, e.storagePoolFree, e.storagePolicyInfo, e.storagePolicyStream, e.mediaAgentInfo,
		e.capacityUsage, e.licenseInfo, e.licenseAmount, e.librarySpace, e.libraryFreeRatio,
	}
}

func (e *Exporter) resetDataMetrics() {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, collector := range []interface{ Reset() }{
		e.vmInfo, e.vmStatus, e.vmStatusCount, e.vmSize, e.vmUsed, e.vmGuest, e.vmBackupStart, e.vmBackupEnd,
		e.commcellInfo, e.slaStatusCount, e.jobs24hCount, e.healthStatusCount, e.entityCount,
		e.jobInfo, e.jobStatus, e.jobPercentComplete, e.jobElapsed, e.jobStart, e.jobLastUpdate, e.jobSizeApplication, e.jobFailedFiles,
		e.alertConfigInfo, e.alertTriggeredInfo, e.alertTriggeredTime, e.alertTriggeredCount, e.alertUnreadCount,
		e.storagePoolInfo, e.storagePoolCapacity, e.storagePoolFree, e.storagePolicyInfo, e.storagePolicyStream, e.mediaAgentInfo,
		e.capacityUsage, e.licenseInfo, e.licenseAmount, e.librarySpace, e.libraryFreeRatio,
	} {
		collector.Reset()
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
