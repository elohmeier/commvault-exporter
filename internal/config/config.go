package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var validLabelName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Config struct {
	Labels                 map[string]string
	DisabledModules        []string
	Timeout                time.Duration
	RefreshInterval        time.Duration
	RefreshTimeout         time.Duration
	MaxStale               time.Duration
	EventLookback          time.Duration
	PageSize               int
	JobCompletedLookupTime int
	AuthMode               string
	Paths                  Paths
}

type Paths struct {
	CommcellDetails           string
	SLA                       string
	Jobs24h                   string
	HealthOverview            string
	Environment               string
	CurrentCapacity           string
	StorageSpaceUsage         string
	LicenseOperatingInstances string
	LicenseEndpointUsers      string
	LicenseHyperscaleStorage  string
	LicenseAirGapProtect      string
	LicenseDataInsights       string
}

const licenseReportDataset = "cc:cr/reportsplusengine/datasets/d7faef75-cf66-40a2-98ce-a2d0cc2a144b:"

func Default() Config {
	return Config{
		Labels:                 map[string]string{},
		Timeout:                30 * time.Second,
		RefreshInterval:        5 * time.Minute,
		RefreshTimeout:         2 * time.Minute,
		MaxStale:               15 * time.Minute,
		EventLookback:          24 * time.Hour,
		PageSize:               1000,
		JobCompletedLookupTime: 86400,
		AuthMode:               "authtoken",
		Paths: Paths{
			CommcellDetails:           "/CustomReportsEngine/rest/reportsplusengine/datasets/a0f077a5-2dfe-4010-a957-57a24cae89a8/data",
			SLA:                       "/CustomReportsEngine/rest/reportsplusengine/datasets/GetSLACounts/data?cache=true&parameter.i_DashboardType=commcell&datasource=2",
			Jobs24h:                   "/CustomReportsEngine/rest/reportsplusengine/datasets/075e703a-b29f-46d6-ad29-7c1a60f7e4f3/data",
			HealthOverview:            "/CustomReportsEngine/rest/reportsplusengine/datasets/b50b20ed-5fc4-4b4c-f7c4-fc6b84eb35cc/data",
			CurrentCapacity:           licenseReportDataset + "feabb5ca-b6b7-4572-b0cb-39352c7e1b67/data/?offset=0&fields=[Dial] AS [Dial],[Purchased] AS [Purchased],[PermTotal] AS [PermTotal],[Eval] AS [Eval],[Usage] AS [Usage],[TermDate] AS [TermDate],[EvalExpiryDate] AS [EvalExpiryDate]&parameter.GUID=-1&limit=5&rawData=false",
			StorageSpaceUsage:         "/CustomReportsEngine/rest/reportsplusengine/datasets/2b366703-52e1-4775-8047-1f4cfa13d2db/data",
			LicenseOperatingInstances: licenseReportDataset + "cd38c52a-e099-4252-d36f-3e2c54540f6f/data/?offset=0&fields=[LicUsageType] AS [License ID],[Dial] AS [License],[Purchased] AS [Available Total (instances)],[PermTotal] AS [Permanent Purchased (instances)],[Eval] AS [Term Purchased (instances)],[Usage] AS [Used (instances)],[EvalExpiryDate] AS [EvalExpiryDate],[Summary] AS [Summary]&parameter.GUID=-1&limit=5",
			LicenseEndpointUsers:      licenseReportDataset + "44cd7de8-ecb2-4ec8-8b2b-162491172eef/data/?offset=0&fields=[LicUsageType] AS [License ID],[Dial] AS [License],[Purchased] AS [Available Total (users)],[PermTotal] AS [Permanent Purchased (users)],[Eval] AS [Term Purchased (users)],[Usage] AS [Used (users)],[EvalExpiryDate] AS [EvalExpiryDate],[Summary] AS [Summary]&parameter.GUID=-1&limit=5",
			LicenseHyperscaleStorage:  licenseReportDataset + "2654b01f-9bb0-481e-b273-4b4fddc585b1/data/?offset=0&fields=[LicUsageType] AS [License ID],[Dial] AS [License],[Purchased] AS [Available Total (TB)],[PermTotal] AS [Permanent Purchased (TB)],[Eval] AS [Term Purchased (TB)],[Usage] AS [Used (TB)],[EvalExpiryDate] AS [EvalExpiryDate],[Summary] AS [Summary]&parameter.GUID=-1&limit=10",
			LicenseAirGapProtect:      licenseReportDataset + "cc2e77ec-9315-4446-cd7e-44ef80a8860e/data/?offset=0&fields=[LicUsageType] AS [License ID],[Dial] AS [License],[Purchased] AS [Available Total (TB)],[PermTotal] AS [Permanent Purchased (TB)],[Eval] AS [Term Purchased (TB)],[Usage] AS [Used (TB)],[EvalExpiryDate] AS [EvalExpiryDate],[Summary] AS [Summary]&parameter.GUID=-1&limit=20",
			LicenseDataInsights:       licenseReportDataset + "f7c6b473-f99d-44b4-ff5e-466b55656500/data/?offset=0&fields=[LicUsageType] AS [License ID],[Dial] AS [License],[Purchased] AS [Available Total],[PermTotal] AS [Permanent Purchased],[Eval] AS [Term Purchased],[Usage] AS [Used],[EvalExpiryDate] AS [EvalExpiryDate],[Summary] AS [Summary]&parameter.GUID=-1&limit=20",
		},
	}
}

func (c Config) IsModuleDisabled(name string) bool {
	for _, m := range c.DisabledModules {
		if m == name {
			return true
		}
	}
	return false
}

func (c Config) LabelKeys() []string {
	keys := make([]string, 0, len(c.Labels))
	for k := range c.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func GetURL() string { return firstEnv("COMMVAULT_URL", "COMMVAULT_BASE_URL") }

func GetCredentials() (username, password string) {
	return os.Getenv("COMMVAULT_USERNAME"), os.Getenv("COMMVAULT_PASSWORD")
}

func GetAuthToken() string { return os.Getenv("COMMVAULT_AUTH_TOKEN") }

func GetAuthMode() string { return os.Getenv("COMMVAULT_AUTH_MODE") }

func GetLabels() string { return os.Getenv("COMMVAULT_LABELS") }

func GetDisabledModules() string { return os.Getenv("COMMVAULT_DISABLED_MODULES") }

func GetCAFile() string { return os.Getenv("COMMVAULT_CA_FILE") }

func GetIgnoreCert() bool {
	return parseBool(os.Getenv("COMMVAULT_IGNORE_CERT")) || parseBool(os.Getenv("COMMVAULT_INSECURE_SKIP_VERIFY"))
}

func GetTimeout() string { return os.Getenv("COMMVAULT_TIMEOUT") }

func GetRefreshInterval() string { return os.Getenv("COMMVAULT_REFRESH_INTERVAL") }

func GetRefreshTimeout() string { return os.Getenv("COMMVAULT_REFRESH_TIMEOUT") }

func GetMaxStale() string { return os.Getenv("COMMVAULT_MAX_STALE") }

func GetEventLookback() string { return os.Getenv("COMMVAULT_EVENT_LOOKBACK") }

func GetPageSize() string { return os.Getenv("COMMVAULT_PAGE_SIZE") }

func GetJobCompletedLookupTime() string { return os.Getenv("COMMVAULT_JOB_COMPLETED_LOOKUP_TIME") }

func ApplyPathEnv(paths Paths) Paths {
	paths.CommcellDetails = chooseEnv(paths.CommcellDetails, "COMMVAULT_ENDPOINT_COMMCELL_DETAILS")
	paths.SLA = chooseEnv(paths.SLA, "COMMVAULT_ENDPOINT_SLA")
	paths.Jobs24h = chooseEnv(paths.Jobs24h, "COMMVAULT_ENDPOINT_JOBS_24H")
	paths.HealthOverview = chooseEnv(paths.HealthOverview, "COMMVAULT_ENDPOINT_HEALTH_OVERVIEW")
	paths.Environment = chooseEnv(paths.Environment, "COMMVAULT_ENDPOINT_ENVIRONMENT")
	paths.CurrentCapacity = chooseEnv(paths.CurrentCapacity, "COMMVAULT_ENDPOINT_CURRENT_CAPACITY")
	paths.StorageSpaceUsage = chooseEnv(paths.StorageSpaceUsage, "COMMVAULT_ENDPOINT_STORAGE_SPACE_USAGE")
	paths.LicenseOperatingInstances = chooseEnv(paths.LicenseOperatingInstances, "COMMVAULT_ENDPOINT_LICENSE_OPERATING_INSTANCES")
	paths.LicenseEndpointUsers = chooseEnv(paths.LicenseEndpointUsers, "COMMVAULT_ENDPOINT_LICENSE_ENDPOINT_USERS")
	paths.LicenseHyperscaleStorage = chooseEnv(paths.LicenseHyperscaleStorage, "COMMVAULT_ENDPOINT_LICENSE_HYPERSCALE_STORAGE")
	paths.LicenseAirGapProtect = chooseEnv(paths.LicenseAirGapProtect, "COMMVAULT_ENDPOINT_LICENSE_AIRGAP_PROTECT")
	paths.LicenseDataInsights = chooseEnv(paths.LicenseDataInsights, "COMMVAULT_ENDPOINT_LICENSE_DATA_INSIGHTS")
	return paths
}

func ParseLabels(labelsStr string) map[string]string {
	labels := make(map[string]string)
	if labelsStr == "" {
		return labels
	}
	for _, pair := range strings.Split(labelsStr, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			labels[key] = value
		}
	}
	return labels
}

func ParseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ValidateLabels(labels map[string]string, reserved []string) error {
	reservedSet := make(map[string]bool, len(reserved))
	for _, label := range reserved {
		reservedSet[label] = true
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !validLabelName.MatchString(key) {
			return fmt.Errorf("invalid label %q: label names must match [A-Za-z_][A-Za-z0-9_]*", key)
		}
		if strings.HasPrefix(key, "__") {
			return fmt.Errorf("invalid label %q: label names starting with __ are reserved", key)
		}
		if reservedSet[key] {
			return fmt.Errorf("invalid label %q: reserved exporter label", key)
		}
	}
	return nil
}

func ChooseCSV(flagValue, envValue string, defaultValue []string) []string {
	switch {
	case flagValue != "":
		return ParseCSV(flagValue)
	case envValue != "":
		return ParseCSV(envValue)
	default:
		return append([]string(nil), defaultValue...)
	}
}

func ChooseDuration(flagValue time.Duration, envValue string, defaultValue time.Duration, name string) (time.Duration, error) {
	value := defaultValue
	if envValue != "" {
		parsed, err := time.ParseDuration(envValue)
		if err != nil {
			return 0, fmt.Errorf("%s must be a duration: %w", name, err)
		}
		value = parsed
	}
	if flagValue != 0 {
		value = flagValue
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}

func ChooseInt(flagValue int, envValue string, defaultValue int, name string) (int, error) {
	value := defaultValue
	if envValue != "" {
		parsed, err := strconv.Atoi(envValue)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer: %w", name, err)
		}
		value = parsed
	}
	if flagValue != 0 {
		value = flagValue
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func chooseEnv(defaultValue string, keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return defaultValue
}
