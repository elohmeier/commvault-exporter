package collector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/elohmeier/commvault-exporter/internal/commvault"
)

func (e *Exporter) collectVMs(ctx context.Context) error {
	vms, err := e.client.GetVMs(ctx)
	if err != nil {
		return err
	}
	statusCounts := map[int]int{}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, vm := range vms {
		name := vm.Name
		if name == "" {
			name = vm.Client.EntityName()
		}
		status := statusName(vm.VMStatus)
		statusCounts[vm.VMStatus]++
		vmLabels := e.baseLabels(
			"vm", name,
			"guid", vm.GUID,
			"client", vm.Client.EntityName(),
			"subclient", vm.Subclient,
			"proxy_client", vm.ProxyClient.EntityName(),
			"vsa_client", vm.PseudoClient.EntityName(),
			"os", vm.OSName,
			"deleted", boolLabel(vm.IsDeleted),
		)
		e.vmInfo.With(vmLabels).Set(1)
		e.vmStatus.With(e.baseLabels("vm", name, "guid", vm.GUID, "status", strconv.Itoa(vm.VMStatus), "status_name", status)).Set(float64(vm.VMStatus))
		e.vmSize.With(e.baseLabels("vm", name, "guid", vm.GUID)).Set(float64(vm.VMSize))
		e.vmUsed.With(e.baseLabels("vm", name, "guid", vm.GUID)).Set(float64(vm.VMUsedSpace))
		e.vmGuest.With(e.baseLabels("vm", name, "guid", vm.GUID)).Set(float64(vm.VMGuestSpace))
		e.vmBackupStart.With(e.baseLabels("vm", name, "guid", vm.GUID)).Set(float64(vm.LastBackupJobInfo.StartTime.Time))
		e.vmBackupEnd.With(e.baseLabels("vm", name, "guid", vm.GUID)).Set(float64(vm.LastBackupJobInfo.EndTime.Time))
	}
	for code, count := range statusCounts {
		e.vmStatusCount.With(e.baseLabels("status", strconv.Itoa(code), "status_name", statusName(code))).Set(float64(count))
	}
	return nil
}

func (e *Exporter) collectDashboard(ctx context.Context) error {
	errs := []error{
		e.runSubcollector(ctx, "dashboard", "commcell_details", e.collectCommcellDetails),
		e.runSubcollector(ctx, "dashboard", "sla", e.collectSLA),
		e.runSubcollector(ctx, "dashboard", "jobs_24h", e.collectJobs24h),
		e.runSubcollector(ctx, "dashboard", "health_overview", e.collectHealth),
	}
	if e.cfg.Paths.Environment != "" {
		errs = append(errs, e.runSubcollector(ctx, "dashboard", "environment", e.collectEnvironment))
	}
	return errors.Join(errs...)
}

func (e *Exporter) collectCommcellDetails(ctx context.Context) error {
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.CommcellDetails)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		e.commcellInfo.With(e.baseLabels("commcell", s(row["CommCellName"]), "version", s(row["Version"]), "release", s(row["ReleaseName"]))).Set(1)
	}
	return nil
}

func (e *Exporter) collectSLA(ctx context.Context) error {
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.SLA)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		e.slaStatusCount.With(e.baseLabels("commcell", s(row["Data Source"]), "status", s(row["SLAStatus"]))).Set(f(row["CurrentCount"]))
	}
	return nil
}

func (e *Exporter) collectJobs24h(ctx context.Context) error {
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.Jobs24h)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		e.jobs24hCount.With(e.baseLabels("commcell", s(row["Data Source"]), "status", s(row["Name"]))).Set(f(row["CurrentCount"]))
	}
	return nil
}

func (e *Exporter) collectHealth(ctx context.Context) error {
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.HealthOverview)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		e.healthStatusCount.With(e.baseLabels("commcell", s(row["Data Source"]), "status", s(row["Status"]))).Set(f(row["Count"]))
	}
	return nil
}

func (e *Exporter) collectEnvironment(ctx context.Context) error {
	if e.cfg.Paths.Environment == "" {
		return nil
	}
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.Environment)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		e.entityCount.With(e.baseLabels("commcell", s(row["Data Source"]), "entity", s(row["PropertyType"]))).Set(f(row["PropertyCount"]))
	}
	return nil
}

func (e *Exporter) collectJobs(ctx context.Context) error {
	jobs, err := e.client.GetJobs(ctx, e.cfg.JobCompletedLookupTime)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, job := range jobs {
		jobID := id(job.JobID)
		status := job.Status
		if status == "" {
			status = job.LocalizedStatus
		}
		client := job.DestClientName
		if client == "" {
			client = job.Subclient.ClientName
		}
		subclient := job.SubclientName
		if subclient == "" {
			subclient = job.Subclient.SubclientName
		}
		app := job.AppTypeName
		if app == "" {
			app = job.Subclient.AppName
		}
		e.jobInfo.With(e.baseLabels("job_id", jobID, "job_type", job.JobType, "operation", job.LocalizedOperationName, "status", status, "client", client, "app", app, "subclient", subclient, "backup_level", job.BackupLevelName)).Set(1)
		e.jobStatus.With(e.baseLabels("job_id", jobID, "status", status)).Set(1)
		e.jobPercentComplete.With(e.baseLabels("job_id", jobID)).Set(job.PercentComplete)
		e.jobElapsed.With(e.baseLabels("job_id", jobID)).Set(float64(job.JobElapsedTime))
		e.jobStart.With(e.baseLabels("job_id", jobID)).Set(float64(job.JobStartTime))
		e.jobLastUpdate.With(e.baseLabels("job_id", jobID)).Set(float64(job.LastUpdateTime))
		e.jobSizeApplication.With(e.baseLabels("job_id", jobID)).Set(float64(job.SizeOfApplication))
		e.jobFailedFiles.With(e.baseLabels("job_id", jobID)).Set(float64(job.TotalFailedFiles))
	}
	return nil
}

func (e *Exporter) collectAlerts(ctx context.Context) error {
	configured, err := e.client.GetAlerts(ctx)
	if err != nil {
		return err
	}
	triggered, err := e.client.GetTriggeredAlerts(ctx)
	if err != nil {
		return err
	}
	counts := map[[2]string]int{}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, alert := range configured.AlertList {
		e.alertConfigInfo.With(e.baseLabels(
			"alert_id", id(alert.Alert.ID),
			"alert", alert.Alert.Name,
			"type", alert.AlertType.Name,
			"category", alert.AlertCategory.Name,
			"creator", alert.Creator.Name,
			"status", id(alert.Status),
		)).Set(1)
	}
	for _, alert := range triggered.AlertsTriggered {
		commcellName := alert.Commcell.DisplayName
		if commcellName == "" {
			commcellName = alert.Commcell.Name
		}
		read := boolLabel(alert.ReadStatus)
		e.alertTriggeredInfo.With(e.baseLabels(
			"alert_id", id(alert.ID),
			"severity", alert.Severity,
			"type", alert.Type,
			"criterion", alert.DetectedCriterion,
			"client", alert.Client.Name,
			"job_id", id(alert.JobID),
			"commcell", commcellName,
			"read", read,
		)).Set(1)
		e.alertTriggeredTime.With(e.baseLabels(
			"alert_id", id(alert.ID),
			"severity", alert.Severity,
			"type", alert.Type,
			"criterion", alert.DetectedCriterion,
			"client", alert.Client.Name,
			"job_id", id(alert.JobID),
			"commcell", commcellName,
			"read", read,
		)).Set(float64(alert.DetectedTime))
		counts[[2]string{alert.Severity, alert.Type}]++
	}
	for key, count := range counts {
		e.alertTriggeredCount.With(e.baseLabels("severity", key[0], "type", key[1])).Set(float64(count))
	}
	e.alertUnreadCount.With(e.baseLabels()).Set(float64(triggered.UnreadCount))
	return nil
}

func (e *Exporter) collectStorage(ctx context.Context) error {
	var pools commvault.StoragePoolsResponse
	var policies commvault.StoragePoliciesResponse
	var mediaAgents commvault.MediaAgentsResponse
	var spaceUsage commvault.TabularResponse
	errs := []error{
		e.runSubcollector(ctx, "storage", "pools", func(ctx context.Context) error {
			var err error
			pools, err = e.client.GetStoragePools(ctx)
			return err
		}),
		e.runSubcollector(ctx, "storage", "policies", func(ctx context.Context) error {
			var err error
			policies, err = e.client.GetStoragePolicies(ctx)
			return err
		}),
		e.runSubcollector(ctx, "storage", "media_agents", func(ctx context.Context) error {
			var err error
			mediaAgents, err = e.client.GetMediaAgents(ctx)
			return err
		}),
		e.runSubcollector(ctx, "storage", "storage_space_usage", func(ctx context.Context) error {
			var err error
			spaceUsage, err = e.client.GetTabular(ctx, e.cfg.Paths.StorageSpaceUsage)
			return err
		}),
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, pool := range pools.StoragePoolList {
		poolID := id(pool.StoragePoolEntity.ID)
		poolName := pool.StoragePoolEntity.Name
		e.storagePoolInfo.With(e.baseLabels("pool_id", poolID, "pool", poolName, "status", pool.Status, "type", id(pool.StoragePoolType), "client_group", pool.StoragePool.ClientGroupName)).Set(1)
		e.storagePoolCapacity.With(e.baseLabels("pool_id", poolID, "pool", poolName)).Set(float64(pool.TotalCapacity))
		e.storagePoolFree.With(e.baseLabels("pool_id", poolID, "pool", poolName)).Set(float64(pool.TotalFreeSpace))
	}
	for _, policy := range policies.Policies {
		policyID := id(policy.StoragePolicyRef.ID)
		policyName := policy.StoragePolicyRef.Name
		e.storagePolicyInfo.With(e.baseLabels("policy_id", policyID, "policy", policyName, "type", id(policy.Type), "copies", id(policy.NumberOfCopies), "plans", joinPlans(policy.Plans))).Set(1)
		e.storagePolicyStream.With(e.baseLabels("policy_id", policyID, "policy", policyName)).Set(float64(policy.NumberOfStreams))
	}
	for _, agent := range mediaAgents.Response {
		e.mediaAgentInfo.With(e.baseLabels("media_agent_id", id(agent.EntityInfo.ID), "media_agent", agent.EntityInfo.Name)).Set(1)
	}
	for _, row := range tableRows(spaceUsage) {
		libraryID := s(row["LibraryId"])
		library := s(row["LibraryName"])
		health := s(row["HealthStatus"])
		e.librarySpace.With(e.baseLabels("library_id", libraryID, "library", library, "health_status", health, "kind", "total")).Set(f(row["TotalSpaceMB"]) * 1024 * 1024)
		e.librarySpace.With(e.baseLabels("library_id", libraryID, "library", library, "health_status", health, "kind", "free")).Set(f(row["TotalFreeSpaceMB"]) * 1024 * 1024)
		e.librarySpace.With(e.baseLabels("library_id", libraryID, "library", library, "health_status", health, "kind", "used")).Set(f(row["TotalUsedSpaceMB"]) * 1024 * 1024)
		e.libraryFreeRatio.With(e.baseLabels("library_id", libraryID, "library", library, "health_status", health)).Set(f(row["FreeSpacePercentage"]) / 100)
	}
	return errors.Join(errs...)
}

func (e *Exporter) collectLicensing(ctx context.Context) error {
	errs := []error{
		e.runSubcollector(ctx, "licensing", "commcell_license", e.collectCommcellLicense),
		e.runSubcollector(ctx, "licensing", "current_capacity", e.collectCurrentCapacity),
		e.runSubcollector(ctx, "licensing", "operating_instances", func(ctx context.Context) error {
			return e.collectLicenseReport(ctx, e.cfg.Paths.LicenseOperatingInstances, "operating_instances", "instances")
		}),
		e.runSubcollector(ctx, "licensing", "endpoint_users", func(ctx context.Context) error {
			return e.collectLicenseReport(ctx, e.cfg.Paths.LicenseEndpointUsers, "endpoint_users", "users")
		}),
		e.runSubcollector(ctx, "licensing", "hyperscale_storage", func(ctx context.Context) error {
			return e.collectLicenseReport(ctx, e.cfg.Paths.LicenseHyperscaleStorage, "hyperscale_storage", "tb")
		}),
		e.runSubcollector(ctx, "licensing", "airgap_protect", func(ctx context.Context) error {
			return e.collectLicenseReport(ctx, e.cfg.Paths.LicenseAirGapProtect, "airgap_protect", "tb")
		}),
		e.runSubcollector(ctx, "licensing", "data_insights", func(ctx context.Context) error {
			return e.collectLicenseReport(ctx, e.cfg.Paths.LicenseDataInsights, "data_insights", "count")
		}),
	}
	return errors.Join(errs...)
}

func (e *Exporter) collectCommcellLicense(ctx context.Context) error {
	license, err := e.client.GetLicenseInfo(ctx)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.commcellLicenseExpiry.With(e.baseLabels(
		"commcell_id", strconv.FormatInt(license.CommCellID, 10),
		"edition", license.Edition,
		"license_mode", license.LicenseMode,
	)).Set(float64(license.ExpiryDate))
	return nil
}

func (e *Exporter) collectCurrentCapacity(ctx context.Context) error {
	if e.cfg.Paths.CurrentCapacity == "" {
		return nil
	}
	resp, err := e.client.GetTabular(ctx, e.cfg.Paths.CurrentCapacity)
	if err != nil {
		return err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, row := range tableRows(resp) {
		dial := s(row["Dial"])
		e.capacityUsage.With(e.baseLabels("dial", dial, "kind", "purchased")).Set(f(row["Purchased"]))
		e.capacityUsage.With(e.baseLabels("dial", dial, "kind", "permanent_total")).Set(f(row["PermTotal"]))
		e.capacityUsage.With(e.baseLabels("dial", dial, "kind", "term_purchased")).Set(f(row["Eval"]))
		e.capacityUsage.With(e.baseLabels("dial", dial, "kind", "usage")).Set(f(row["Usage"]))
	}
	return nil
}

func (e *Exporter) collectLicenseReport(ctx context.Context, endpoint, report, unit string) error {
	if endpoint == "" {
		return nil
	}
	resp, err := e.client.GetTabular(ctx, endpoint)
	if err != nil {
		return err
	}
	type parsedLicenseRow struct {
		row    map[string]any
		expiry float64
	}
	rows := tableRows(resp)
	parsed := make([]parsedLicenseRow, 0, len(rows))
	for _, row := range rows {
		expiryDate := firstString(row, "EvalExpiryDate")
		expiry, err := parseLicenseExpiryDate(expiryDate)
		if err != nil {
			return fmt.Errorf("license report %s license_id=%q expiry_date=%q: %w", report, firstString(row, "License ID", "LicUsageType"), expiryDate, err)
		}
		parsed = append(parsed, parsedLicenseRow{row: row, expiry: expiry})
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for _, item := range parsed {
		e.collectLicenseRowLocked(report, unit, item.row, item.expiry)
	}
	return nil
}

func (e *Exporter) collectLicenseRowLocked(report, unit string, row map[string]any, expiry float64) {
	licenseID := firstString(row, "License ID", "LicUsageType")
	license := firstString(row, "License", "Dial")
	summary := firstString(row, "Summary")
	evalExpiryDate := firstString(row, "EvalExpiryDate")
	infoLabels := e.baseLabels(
		"license_id", licenseID,
		"license", license,
		"report", report,
		"unit", unit,
		"summary", summary,
		"eval_expiry_date", evalExpiryDate,
	)
	e.licenseInfo.With(infoLabels).Set(1)
	e.licenseExpiry.With(e.baseLabels(
		"license_id", licenseID,
		"license", license,
		"report", report,
		"unit", unit,
	)).Set(expiry)
	for _, amount := range []struct {
		kind string
		base string
	}{
		{kind: "available_total", base: "Available Total"},
		{kind: "permanent_purchased", base: "Permanent Purchased"},
		{kind: "term_purchased", base: "Term Purchased"},
		{kind: "used", base: "Used"},
	} {
		e.licenseAmount.With(e.baseLabels(
			"license_id", licenseID,
			"license", license,
			"report", report,
			"unit", unit,
			"kind", amount.kind,
		)).Set(f(licenseColumnValue(row, amount.base, unit)))
	}
}

func parseLicenseExpiryDate(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	expiry, err := time.ParseInLocation("02 Jan 2006", value, time.UTC)
	if err != nil {
		return 0, err
	}
	return float64(expiry.Unix()), nil
}

func licenseColumnValue(row map[string]any, base, unit string) any {
	if suffix := licenseUnitColumnSuffix(unit); suffix != "" {
		if value, ok := row[base+" ("+suffix+")"]; ok {
			return value
		}
	}
	if value, ok := row[base]; ok {
		return value
	}
	switch base {
	case "Available Total":
		return row["Purchased"]
	case "Permanent Purchased":
		return row["PermTotal"]
	case "Term Purchased":
		return row["Eval"]
	case "Used":
		return row["Usage"]
	default:
		return nil
	}
}

func licenseUnitColumnSuffix(unit string) string {
	switch unit {
	case "instances", "users":
		return unit
	case "tb":
		return "TB"
	default:
		return ""
	}
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return s(value)
		}
	}
	return ""
}

var _ = commvault.VMInfo{}
