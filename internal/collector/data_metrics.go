package collector

import (
	"github.com/elohmeier/commvault-exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

type dataMetrics struct {
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

func newDataMetrics(cfg config.Config) *dataMetrics {
	g := func(name, help string, labels []string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help}, withBaseLabels(cfg, labels))
	}
	return &dataMetrics{
		vmInfo:                g("vm_info", "Commvault VM inventory metadata.", []string{"vm", "guid", "client", "subclient", "proxy_client", "vsa_client", "os", "deleted"}),
		vmStatus:              g("vm_status", "Commvault VM backup status code.", []string{"vm", "guid", "status", "status_name"}),
		vmStatusCount:         g("vm_status_count", "Commvault VM count by backup status.", []string{"status", "status_name"}),
		vmSize:                g("vm_size_bytes", "Commvault VM allocated size in bytes.", []string{"vm", "guid"}),
		vmUsed:                g("vm_used_space_bytes", "Commvault VM used disk space in bytes.", []string{"vm", "guid"}),
		vmGuest:               g("vm_guest_space_bytes", "Commvault VM guest-used space in bytes.", []string{"vm", "guid"}),
		vmBackupStart:         g("vm_last_backup_start_time_seconds", "Commvault VM last backup start timestamp.", []string{"vm", "guid"}),
		vmBackupEnd:           g("vm_last_backup_end_time_seconds", "Commvault VM last backup end timestamp.", []string{"vm", "guid"}),
		commcellInfo:          g("commcell_info", "Commvault CommCell metadata.", []string{"commcell", "version", "release"}),
		slaStatusCount:        g("sla_status_count", "Commvault SLA status counts.", []string{"commcell", "status"}),
		jobs24hCount:          g("jobs_24h_count", "Commvault dashboard job counts for the current 24h window.", []string{"commcell", "status"}),
		healthStatusCount:     g("health_status_count", "Commvault health overview counts.", []string{"commcell", "status"}),
		entityCount:           g("environment_entity_count", "Commvault environment entity counts.", []string{"commcell", "entity"}),
		jobInfo:               g("job_info", "Commvault job metadata.", []string{"job_id", "job_type", "operation", "status", "client", "app", "subclient", "backup_level"}),
		jobStatus:             g("job_status", "Commvault job status as a one-hot gauge.", []string{"job_id", "status"}),
		jobPercentComplete:    g("job_percent_complete", "Commvault job percent complete.", []string{"job_id"}),
		jobElapsed:            g("job_elapsed_seconds", "Commvault job elapsed seconds.", []string{"job_id"}),
		jobStart:              g("job_start_time_seconds", "Commvault job start timestamp.", []string{"job_id"}),
		jobLastUpdate:         g("job_last_update_time_seconds", "Commvault job last update timestamp.", []string{"job_id"}),
		jobSizeApplication:    g("job_size_application_bytes", "Commvault job application size in bytes.", []string{"job_id"}),
		jobFailedFiles:        g("job_failed_files", "Commvault job failed file count.", []string{"job_id"}),
		alertConfigInfo:       g("alert_config_info", "Commvault configured alert metadata.", []string{"alert_id", "alert", "type", "category", "creator", "status"}),
		alertTriggeredInfo:    g("alert_triggered_info", "Commvault triggered alert metadata.", []string{"alert_id", "severity", "type", "criterion", "client", "job_id", "commcell", "read"}),
		alertTriggeredTime:    g("alert_triggered_detected_time_seconds", "Commvault triggered alert detected timestamp.", []string{"alert_id", "severity", "type", "criterion", "client", "job_id", "commcell", "read"}),
		alertTriggeredCount:   g("alert_triggered_count", "Commvault triggered alert count by severity and type.", []string{"severity", "type"}),
		alertUnreadCount:      g("alert_unread_count", "Commvault unread triggered alert count.", nil),
		storageAccessEvents:   g("storage_access_event_count", "Commvault storage access events in the configured lookup window.", []string{"event_code", "event_type", "severity", "client", "mount_path"}),
		storageAccessLast:     g("storage_access_event_last_timestamp_seconds", "Unix timestamp of the latest matching Commvault storage access event.", []string{"event_code", "event_type", "severity", "client", "mount_path"}),
		storageAccessTries:    g("storage_access_event_latest_attempts", "Attempt count reported by the latest matching Commvault storage access event.", []string{"event_code", "event_type", "severity", "client", "mount_path"}),
		storagePoolInfo:       g("storage_pool_info", "Commvault storage pool metadata.", []string{"pool_id", "pool", "status", "type", "client_group"}),
		storagePoolCapacity:   g("storage_pool_capacity_bytes", "Commvault storage pool total capacity in bytes.", []string{"pool_id", "pool"}),
		storagePoolFree:       g("storage_pool_free_bytes", "Commvault storage pool free bytes.", []string{"pool_id", "pool"}),
		storagePolicyInfo:     g("storage_policy_info", "Commvault storage policy metadata.", []string{"policy_id", "policy", "type", "copies", "plans"}),
		storagePolicyStream:   g("storage_policy_streams", "Commvault storage policy stream count.", []string{"policy_id", "policy"}),
		mediaAgentInfo:        g("media_agent_info", "Commvault media agent metadata.", []string{"media_agent_id", "media_agent"}),
		capacityUsage:         g("capacity_usage", "Commvault capacity usage by dial.", []string{"dial", "kind"}),
		capacityExpiry:        g("capacity_license_expiry_timestamp_seconds", "Unix timestamp of the capacity license expiry reported by Commvault; 0 means no expiry was supplied.", []string{"dial"}),
		licenseInfo:           g("license_info", "Commvault license report metadata.", []string{"license_id", "license", "report", "unit", "summary", "eval_expiry_date"}),
		licenseAmount:         g("license_amount", "Commvault license amount by report, license, unit, and kind.", []string{"license_id", "license", "report", "unit", "kind"}),
		librarySpace:          g("library_space_bytes", "Commvault library space by kind.", []string{"library_id", "library", "health_status", "kind"}),
		libraryFreeRatio:      g("library_free_ratio", "Commvault library free-space ratio.", []string{"library_id", "library", "health_status"}),
		libraryInfo:           g("library_info", "Commvault library metadata from the library inventory and details APIs.", []string{"library_id", "library", "type", "status"}),
		libraryReady:          g("library_ready", "Whether the Commvault library reports a Ready or Online state.", []string{"library_id", "library"}),
		libraryMountPaths:     g("library_mount_paths", "Commvault library mount path count by kind.", []string{"library_id", "library", "kind"}),
		mountPathInfo:         g("mount_path_info", "Commvault mount path metadata.", []string{"library_id", "library", "mount_path_id", "mount_path", "status"}),
		mountPathReady:        g("mount_path_ready", "Whether the Commvault mount path status begins with Ready.", []string{"library_id", "library", "mount_path_id", "mount_path"}),
		mountPathWriteOff:     g("mount_path_disabled_for_new_write", "Whether the Commvault mount path is disabled for new writes.", []string{"library_id", "library", "mount_path_id", "mount_path"}),
		mountPathLogCaching:   g("mount_path_used_for_log_caching", "Whether the Commvault mount path is used for log caching.", []string{"library_id", "library", "mount_path_id", "mount_path"}),
		commcellLicenseExpiry: g("commcell_license_expiry_timestamp_seconds", "Unix timestamp when the current CommCell license expires; 0 means no expiry was supplied.", []string{"commcell_id", "edition", "license_mode"}),
		licenseExpiry:         g("license_expiry_timestamp_seconds", "Unix timestamp of the license expiry reported by Commvault; 0 means no expiry was supplied.", []string{"license_id", "license", "report", "unit"}),
	}
}

func (m *dataMetrics) moduleCollectors(module string) []prometheus.Collector {
	switch module {
	case "vm":
		return []prometheus.Collector{m.vmInfo, m.vmStatus, m.vmStatusCount, m.vmSize, m.vmUsed, m.vmGuest, m.vmBackupStart, m.vmBackupEnd}
	case "dashboard":
		return []prometheus.Collector{m.commcellInfo, m.slaStatusCount, m.jobs24hCount, m.healthStatusCount, m.entityCount}
	case "jobs":
		return []prometheus.Collector{m.jobInfo, m.jobStatus, m.jobPercentComplete, m.jobElapsed, m.jobStart, m.jobLastUpdate, m.jobSizeApplication, m.jobFailedFiles}
	case "alerts":
		return []prometheus.Collector{m.alertConfigInfo, m.alertTriggeredInfo, m.alertTriggeredTime, m.alertTriggeredCount, m.alertUnreadCount}
	case "events":
		return []prometheus.Collector{m.storageAccessEvents, m.storageAccessLast, m.storageAccessTries}
	case "storage":
		return []prometheus.Collector{m.storagePoolInfo, m.storagePoolCapacity, m.storagePoolFree, m.storagePolicyInfo, m.storagePolicyStream, m.mediaAgentInfo, m.librarySpace, m.libraryFreeRatio, m.libraryInfo, m.libraryReady, m.libraryMountPaths, m.mountPathInfo, m.mountPathReady, m.mountPathWriteOff, m.mountPathLogCaching}
	case "licensing":
		return []prometheus.Collector{m.capacityUsage, m.capacityExpiry, m.commcellLicenseExpiry, m.licenseInfo, m.licenseAmount, m.licenseExpiry}
	default:
		return nil
	}
}

func (m *dataMetrics) allCollectors() []prometheus.Collector {
	var collectors []prometheus.Collector
	for _, module := range moduleNames {
		collectors = append(collectors, m.moduleCollectors(module)...)
	}
	return collectors
}

func (e *Exporter) useDataMetrics(m *dataMetrics) {
	e.vmInfo = m.vmInfo
	e.vmStatus = m.vmStatus
	e.vmStatusCount = m.vmStatusCount
	e.vmSize = m.vmSize
	e.vmUsed = m.vmUsed
	e.vmGuest = m.vmGuest
	e.vmBackupStart = m.vmBackupStart
	e.vmBackupEnd = m.vmBackupEnd
	e.commcellInfo = m.commcellInfo
	e.slaStatusCount = m.slaStatusCount
	e.jobs24hCount = m.jobs24hCount
	e.healthStatusCount = m.healthStatusCount
	e.entityCount = m.entityCount
	e.jobInfo = m.jobInfo
	e.jobStatus = m.jobStatus
	e.jobPercentComplete = m.jobPercentComplete
	e.jobElapsed = m.jobElapsed
	e.jobStart = m.jobStart
	e.jobLastUpdate = m.jobLastUpdate
	e.jobSizeApplication = m.jobSizeApplication
	e.jobFailedFiles = m.jobFailedFiles
	e.alertConfigInfo = m.alertConfigInfo
	e.alertTriggeredInfo = m.alertTriggeredInfo
	e.alertTriggeredTime = m.alertTriggeredTime
	e.alertTriggeredCount = m.alertTriggeredCount
	e.alertUnreadCount = m.alertUnreadCount
	e.storageAccessEvents = m.storageAccessEvents
	e.storageAccessLast = m.storageAccessLast
	e.storageAccessTries = m.storageAccessTries
	e.storagePoolInfo = m.storagePoolInfo
	e.storagePoolCapacity = m.storagePoolCapacity
	e.storagePoolFree = m.storagePoolFree
	e.storagePolicyInfo = m.storagePolicyInfo
	e.storagePolicyStream = m.storagePolicyStream
	e.mediaAgentInfo = m.mediaAgentInfo
	e.capacityUsage = m.capacityUsage
	e.capacityExpiry = m.capacityExpiry
	e.licenseInfo = m.licenseInfo
	e.licenseAmount = m.licenseAmount
	e.librarySpace = m.librarySpace
	e.libraryFreeRatio = m.libraryFreeRatio
	e.libraryInfo = m.libraryInfo
	e.libraryReady = m.libraryReady
	e.libraryMountPaths = m.libraryMountPaths
	e.mountPathInfo = m.mountPathInfo
	e.mountPathReady = m.mountPathReady
	e.mountPathWriteOff = m.mountPathWriteOff
	e.mountPathLogCaching = m.mountPathLogCaching
	e.commcellLicenseExpiry = m.commcellLicenseExpiry
	e.licenseExpiry = m.licenseExpiry
}
