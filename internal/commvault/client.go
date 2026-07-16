package commvault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	systemCertPool = x509.SystemCertPool
	readFile       = os.ReadFile
	newRequest     = http.NewRequestWithContext
)

type Config struct {
	BaseURL            string
	Username           string
	Password           string
	AuthToken          string
	AuthMode           string
	Timeout            time.Duration
	PageSize           int
	InsecureSkipVerify bool
	CAFile             string
	UserAgent          string
	Metrics            *Metrics
}

type Client struct {
	baseURL    *url.URL
	username   string
	password   string
	token      string
	authMode   string
	pageSize   int
	userAgent  string
	httpClient *http.Client
	metrics    *Metrics
	mu         sync.Mutex
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e APIError) Error() string {
	return fmt.Sprintf("commvault api status=%d body=%s", e.StatusCode, e.Body)
}

type TabularFailureError struct {
	Endpoint string
	Failures map[string]any
}

func (e TabularFailureError) Error() string {
	data, err := json.Marshal(e.Failures)
	if err != nil {
		return fmt.Sprintf("commvault tabular endpoint %s returned failures", e.Endpoint)
	}
	return fmt.Sprintf("commvault tabular endpoint %s returned failures: %s", e.Endpoint, data)
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.PageSize <= 0 {
		cfg.PageSize = 1000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "commvault-exporter/dev"
	}
	cfg.AuthMode = strings.ToLower(strings.TrimSpace(cfg.AuthMode))
	if cfg.AuthMode == "" {
		cfg.AuthMode = "authtoken"
	}
	if cfg.AuthMode != "authtoken" && cfg.AuthMode != "bearer" {
		return nil, fmt.Errorf("auth mode must be authtoken or bearer, got %q", cfg.AuthMode)
	}
	baseURL, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("base URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, errors.New("base URL must include host")
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec
	if cfg.CAFile != "" {
		pool, err := systemCertPool()
		if err != nil {
			return nil, err
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		caData, err := readFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to append CA certificates from %s", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}
	return &Client{
		baseURL:    baseURL,
		username:   cfg.Username,
		password:   cfg.Password,
		token:      cfg.AuthToken,
		authMode:   cfg.AuthMode,
		pageSize:   cfg.PageSize,
		userAgent:  cfg.UserAgent,
		httpClient: &http.Client{Timeout: cfg.Timeout, Transport: transport},
		metrics:    cfg.Metrics,
	}, nil
}

func (c *Client) Hostname() string {
	return c.baseURL.Hostname()
}

func (c *Client) EnsureLogin(ctx context.Context) error {
	c.mu.Lock()
	hasToken := c.token != ""
	c.mu.Unlock()
	if hasToken {
		return nil
	}
	return c.Login(ctx)
}

func (c *Client) Login(ctx context.Context) error {
	if c.username == "" || c.password == "" {
		return errors.New("credentials are required when COMMVAULT_AUTH_TOKEN is not set")
	}
	body := map[string]any{
		"username": c.username,
		"password": base64.StdEncoding.EncodeToString([]byte(c.password)),
		"timeout":  30,
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "Login", nil, nil, body, &raw, false); err != nil {
		return err
	}
	var resp LoginResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	token := strings.TrimSpace(resp.Token)
	if token == "" {
		token = strings.TrimSpace(resp.AuthToken)
	}
	if token == "" {
		token = strings.TrimSpace(resp.Data.Token)
	}
	if token == "" {
		token = strings.TrimSpace(resp.Data.AuthToken)
	}
	if token == "" {
		return fmt.Errorf("login response did not contain token, authToken, data.token, or data.authToken%s", loginResponseDetail(resp, raw))
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
	return nil
}

func loginResponseDetail(resp LoginResponse, raw json.RawMessage) string {
	var details []string
	add := func(label, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			details = append(details, label+"="+value)
		}
	}
	if resp.ErrorCode != 0 {
		details = append(details, fmt.Sprintf("errorCode=%d", resp.ErrorCode))
	}
	add("errorMessage", resp.ErrorMessage)
	add("errLogMessage", resp.ErrLogMessage)
	add("message", resp.Message)
	for _, item := range resp.ErrList {
		if item.ErrorCode != 0 {
			details = append(details, fmt.Sprintf("errList.errorCode=%d", item.ErrorCode))
		}
		add("errList.errorMessage", item.ErrorMessage)
		add("errList.errLogMessage", item.ErrLogMessage)
		add("errList.message", item.Message)
	}
	if resp.AccountLocked {
		details = append(details, "isAccountLocked=true")
	}
	if resp.ForcePwdChange {
		details = append(details, "forcePasswordChange=true")
	}
	if resp.LoginAttempts != 0 {
		details = append(details, fmt.Sprintf("loginAttempts=%d", resp.LoginAttempts))
	}
	if resp.RemainingLock != 0 {
		details = append(details, fmt.Sprintf("remainingLockTime=%d", resp.RemainingLock))
	}
	if len(details) > 0 {
		return ": " + strings.Join(details, "; ")
	}
	keys := jsonObjectKeys(raw)
	if len(keys) > 0 {
		return "; response keys: " + strings.Join(keys, ", ")
	}
	return ""
}

func jsonObjectKeys(raw json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) == 0 {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (c *Client) GetVMs(ctx context.Context) ([]VMInfo, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return nil, err
	}
	var all []VMInfo
	for page := 0; ; page++ {
		var resp VMResponse
		headers := http.Header{}
		headers.Set("PagingInfo", fmt.Sprintf("%d,%d", page, c.pageSize))
		if err := c.do(ctx, http.MethodGet, "VM", nil, headers, nil, &resp, true); err != nil {
			return nil, err
		}
		all = append(all, resp.VMStatusInfoList...)
		if resp.TotalRecords <= 0 || len(all) >= resp.TotalRecords || len(resp.VMStatusInfoList) < c.pageSize {
			break
		}
	}
	return all, nil
}

func (c *Client) GetJobs(ctx context.Context, completedLookupTime int) ([]JobSummary, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return nil, err
	}
	var all []JobSummary
	for offset := 0; ; offset += c.pageSize {
		query := url.Values{}
		if completedLookupTime > 0 {
			query.Set("completedJobLookupTime", strconv.Itoa(completedLookupTime))
		}
		headers := http.Header{}
		headers.Set("limit", strconv.Itoa(c.pageSize))
		headers.Set("offset", strconv.Itoa(offset))
		var resp JobsResponse
		if err := c.do(ctx, http.MethodGet, "Job", query, headers, nil, &resp, true); err != nil {
			return nil, err
		}
		for _, job := range resp.Jobs {
			all = append(all, job.Summary)
		}
		if resp.TotalRecordsWithoutPaging <= 0 || len(all) >= resp.TotalRecordsWithoutPaging || len(resp.Jobs) < c.pageSize {
			break
		}
	}
	return all, nil
}

func (c *Client) GetTabular(ctx context.Context, endpoint string) (TabularResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return TabularResponse{}, err
	}
	var resp TabularResponse
	if err := c.do(ctx, http.MethodGet, endpoint, nil, nil, nil, &resp, true); err != nil {
		return resp, err
	}
	if len(resp.Failures) > 0 {
		return resp, TabularFailureError{Endpoint: endpoint, Failures: resp.Failures}
	}
	return resp, nil
}

func (c *Client) GetAlerts(ctx context.Context) (AlertsResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return AlertsResponse{}, err
	}
	var resp AlertsResponse
	err := c.do(ctx, http.MethodGet, "AlertRule", nil, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) GetTriggeredAlerts(ctx context.Context) (TriggeredAlertsResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return TriggeredAlertsResponse{}, err
	}
	var resp TriggeredAlertsResponse
	err := c.do(ctx, http.MethodGet, "V4/TriggeredAlerts", nil, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) GetLicenseInfo(ctx context.Context) (LicenseInfoResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return LicenseInfoResponse{}, err
	}
	var resp LicenseInfoResponse
	err := c.do(ctx, http.MethodGet, "/webconsole/api/V4/License", nil, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) GetStoragePools(ctx context.Context) (StoragePoolsResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return StoragePoolsResponse{}, err
	}
	var resp StoragePoolsResponse
	err := c.do(ctx, http.MethodGet, "cc:StoragePool", nil, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) GetStoragePolicies(ctx context.Context) (StoragePoliciesResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return StoragePoliciesResponse{}, err
	}
	var resp StoragePoliciesResponse
	query := url.Values{"propertyLevel": []string{"20"}}
	err := c.do(ctx, http.MethodGet, "cc:V2/StoragePolicy", query, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) GetMediaAgents(ctx context.Context) (MediaAgentsResponse, error) {
	if err := c.EnsureLogin(ctx); err != nil {
		return MediaAgentsResponse{}, err
	}
	var resp MediaAgentsResponse
	err := c.do(ctx, http.MethodGet, "cc:MediaAgent", nil, nil, nil, &resp, true)
	return resp, err
}

func (c *Client) do(ctx context.Context, method, endpoint string, query url.Values, headers http.Header, body any, dest any, auth bool) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(encoded)
	}
	u := c.apiURL(endpoint, query)
	req, err := newRequest(ctx, method, u.String(), reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if auth {
		c.mu.Lock()
		token := c.token
		authMode := c.authMode
		c.mu.Unlock()
		switch authMode {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+token)
		default:
			req.Header.Set("Authtoken", token)
		}
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start).Seconds()
	if err != nil {
		c.metrics.observe(endpoint, "error", elapsed)
		return err
	}
	defer resp.Body.Close()
	c.metrics.observe(endpoint, strconv.Itoa(resp.StatusCode), elapsed)

	if auth && resp.StatusCode == http.StatusUnauthorized && c.username != "" && c.password != "" {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.Login(ctx); err != nil {
			return err
		}
		return c.do(ctx, method, endpoint, query, headers, body, dest, auth)
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	if dest == nil {
		return nil
	}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	return decoder.Decode(dest)
}

func (c *Client) apiURL(endpoint string, query url.Values) url.URL {
	u := *c.baseURL
	endpointPath := endpoint
	endpointQuery := ""
	if idx := strings.Index(endpointPath, "?"); idx >= 0 {
		endpointQuery = endpointPath[idx+1:]
		endpointPath = endpointPath[:idx]
	}
	basePath := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasPrefix(endpointPath, "/"):
		basePath = trimKnownAPIBase(basePath)
		u.Path = strings.TrimRight(basePath, "/") + endpointPath
	case strings.HasPrefix(endpointPath, "cc:"):
		basePath = trimKnownAPIBase(basePath)
		if !strings.HasSuffix(basePath, "/commandcenter/api") {
			basePath += "/commandcenter/api"
		}
		u.Path = basePath + "/" + strings.TrimLeft(strings.TrimPrefix(endpointPath, "cc:"), "/")
	default:
		if !hasKnownAPIBase(basePath) {
			basePath += "/webconsole/api"
		}
		u.Path = basePath + "/" + strings.TrimLeft(endpointPath, "/")
	}
	values := url.Values{}
	if endpointQuery != "" {
		parsed, err := url.ParseQuery(endpointQuery)
		if err == nil {
			for key, vals := range parsed {
				for _, val := range vals {
					values.Add(key, val)
				}
			}
		} else {
			u.RawQuery = endpointQuery
		}
	}
	for key, vals := range query {
		for _, val := range vals {
			values.Add(key, val)
		}
	}
	if u.RawQuery == "" {
		u.RawQuery = values.Encode()
	}
	return u
}

func trimKnownAPIBase(path string) string {
	for _, suffix := range []string{"/webconsole/api", "/commandcenter/api"} {
		if strings.HasSuffix(path, suffix) {
			return strings.TrimSuffix(path, suffix)
		}
	}
	return path
}

func hasKnownAPIBase(path string) bool {
	for _, suffix := range []string{"/webconsole/api", "/commandcenter/api"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
