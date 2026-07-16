# commvault-exporter

[![CI](https://github.com/elohmeier/commvault-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/elohmeier/commvault-exporter/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/elohmeier/commvault-exporter)](https://github.com/elohmeier/commvault-exporter/releases)
[![GHCR](https://img.shields.io/badge/ghcr.io-commvault--exporter-blue)](https://github.com/users/elohmeier/packages/container/package/commvault-exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/elohmeier/commvault-exporter)](https://goreportcard.com/report/github.com/elohmeier/commvault-exporter)
[![Go Reference](https://pkg.go.dev/badge/github.com/elohmeier/commvault-exporter.svg)](https://pkg.go.dev/github.com/elohmeier/commvault-exporter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Prometheus exporter for Commvault Backup and Recovery / CommCell REST APIs.

The exporter logs in to Commvault, refreshes data in the background, and serves
Prometheus metrics from an in-process cache. It exposes `/metrics`, `/health`,
`/readyz`, and `/debug/cache`.

## Quick Start

```sh
export COMMVAULT_USERNAME='<readonly-user>'
export COMMVAULT_PASSWORD='<password>'
go run . -url https://commvault.example.com
```

The exporter listens on `:9720` by default.

## Configuration

| Flag | Environment | Default | Description |
| --- | --- | --- | --- |
| `-url` | `COMMVAULT_URL` | required | Commvault Web Console or Command Center base URL. |
| `-auth-mode` | `COMMVAULT_AUTH_MODE` | `authtoken` | Authentication header mode: `authtoken` or `bearer`. |
| `-labels` | `COMMVAULT_LABELS` | none | Comma-separated Prometheus const labels. |
| `-disabled-modules` | `COMMVAULT_DISABLED_MODULES` | none | Comma-separated modules to disable. |
| `-bind-port` | none | `9720` | HTTP port. |
| `-page-size` | `COMMVAULT_PAGE_SIZE` | `1000` | API page size. |
| `-timeout` | `COMMVAULT_TIMEOUT` | `30s` | Per-request timeout. |
| `-refresh-interval` | `COMMVAULT_REFRESH_INTERVAL` | `5m` | Background refresh interval. |
| `-refresh-timeout` | `COMMVAULT_REFRESH_TIMEOUT` | `2m` | Timeout for one full refresh. |
| `-max-stale` | `COMMVAULT_MAX_STALE` | `15m` | Maximum cache age before readiness fails. |
| `-job-completed-lookup-time` | `COMMVAULT_JOB_COMPLETED_LOOKUP_TIME` | `86400` | Commvault job lookup window in seconds. |
| `-ignore-cert` | `COMMVAULT_IGNORE_CERT` | `false` | Disable TLS certificate verification. |
| `-ca-file` | `COMMVAULT_CA_FILE` | none | Custom CA bundle path. |

Credentials are read from `COMMVAULT_USERNAME` and `COMMVAULT_PASSWORD`.
Set `COMMVAULT_PASSWORD` to the real password; the exporter encodes it for the
Commvault Login API request.
Set `COMMVAULT_AUTH_TOKEN` to use a pre-created token instead of logging in.

Disable collectors by these names: `vm`, `dashboard`, `jobs`, `alerts`,
`storage`, `licensing`.

Report-backed dashboard/storage/licensing endpoints are configurable because
Commvault publishes some of them as report dataset paths. Override them with:
`COMMVAULT_ENDPOINT_COMMCELL_DETAILS`, `COMMVAULT_ENDPOINT_SLA`,
`COMMVAULT_ENDPOINT_JOBS_24H`, `COMMVAULT_ENDPOINT_HEALTH_OVERVIEW`,
`COMMVAULT_ENDPOINT_ENVIRONMENT`, `COMMVAULT_ENDPOINT_CURRENT_CAPACITY`, and
`COMMVAULT_ENDPOINT_STORAGE_SPACE_USAGE`.

Licensing report endpoints can also be overridden with
`COMMVAULT_ENDPOINT_LICENSE_OPERATING_INSTANCES`,
`COMMVAULT_ENDPOINT_LICENSE_ENDPOINT_USERS`,
`COMMVAULT_ENDPOINT_LICENSE_HYPERSCALE_STORAGE`,
`COMMVAULT_ENDPOINT_LICENSE_AIRGAP_PROTECT`, and
`COMMVAULT_ENDPOINT_LICENSE_DATA_INSIGHTS`. Current capacity is collected by
the `licensing` module and still emits the compatibility metric
`commvault_capacity_usage`.

The licensing module also calls `GET /webconsole/api/V4/License` and exports:

- `commvault_commcell_license_expiry_timestamp_seconds` for the exact CommCell
  license expiry returned by the API. Its labels are `commcell_id`, `edition`,
  and `license_mode`.
- `commvault_license_expiry_timestamp_seconds` for each report-backed license.
  Report dates use the Commvault `02 Jan 2006` format and are interpreted as
  midnight UTC. The existing `eval_expiry_date` label on
  `commvault_license_info` is retained for compatibility.

An empty expiry, the API value `0`, or the report sentinel `01 Jan 1970` is
exported as `0`. Guard alerts with `> 0`, for example:

```promql
commvault_commcell_license_expiry_timestamp_seconds > 0
and
commvault_commcell_license_expiry_timestamp_seconds - time() < 60 * 24 * 60 * 60
```

The capacity report's raw `Eval` column represents term-purchased capacity.
The corresponding series is therefore exposed as
`commvault_capacity_usage{kind="term_purchased"}`. This is a breaking label
rename from `kind="evaluation"`; update queries and alerts when upgrading.

The Data Insights report keeps its existing generic field mapping until it can
be validated against a licensed CommCell UI/API response.

`COMMVAULT_ENDPOINT_ENVIRONMENT` has no built-in default because the observed
Commvault environment dataset returns a report-engine `CacheDB` bad-request
failure. The environment entity-count metrics are collected only when this
variable points to a working dataset.
