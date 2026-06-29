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
| `-job-completed-lookup-time` | `COMMVAULT_JOB_COMPLETED_LOOKUP_TIME` | `31536000` | Commvault job lookup window in seconds. |
| `-ignore-cert` | `COMMVAULT_IGNORE_CERT` | `false` | Disable TLS certificate verification. |
| `-ca-file` | `COMMVAULT_CA_FILE` | none | Custom CA bundle path. |

Credentials are read from `COMMVAULT_USERNAME` and `COMMVAULT_PASSWORD`.
Set `COMMVAULT_PASSWORD` to the real password; the exporter encodes it for the
Commvault Login API request.
Set `COMMVAULT_AUTH_TOKEN` to use a pre-created token instead of logging in.

Disable collectors by these names: `vm`, `dashboard`, `jobs`, `alerts`,
`storage`.

Report-backed dashboard/storage endpoints are configurable because Commvault
publishes some of them as report dataset paths. Override them with:
`COMMVAULT_ENDPOINT_COMMCELL_DETAILS`, `COMMVAULT_ENDPOINT_SLA`,
`COMMVAULT_ENDPOINT_JOBS_24H`, `COMMVAULT_ENDPOINT_HEALTH_OVERVIEW`,
`COMMVAULT_ENDPOINT_ENVIRONMENT`, `COMMVAULT_ENDPOINT_CURRENT_CAPACITY`, and
`COMMVAULT_ENDPOINT_STORAGE_SPACE_USAGE`.
