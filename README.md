# container-procmon

`procmon` is a lightweight, per-process resource monitor for **Container Apps** and **App Service on Linux**, written in Go. It samples every process under `/proc`, computes CPU and memory utilization, and exports the results as OpenTelemetry metrics. It runs as a standalone binary alongside your app. See the [Dockerfile](Dockerfile) for how to build it and inject it into your image.

These PaaS services don't expose the underlying nodes, so per-process metrics have to be gathered from within the app itself. Since you also can't reliably target individual instances or replicas, `procmon` pushes metrics out rather than relying on the traditional Prometheus pull model.

## Metrics

| Metric | OTel name | Unit | Description |
| --- | --- | --- | --- |
| CPU utilization | `process.cpu.utilization` | ratio `[0,1]` | Per-process CPU usage since the last sample, derived from `/proc/<pid>/stat` ticks. |
| Memory utilization | `process.memory.utilization` | ratio `[0,1]` | RSS relative to the container's cgroup memory limit. |
| Memory RSS | `process.memory.rss` | bytes | Resident set size of the process. |

Each sample is tagged with `instance_id`, `pid`, and `name`. The `instance_id` is auto-detected from the Azure environment (`CONTAINER_APP_REPLICA_NAME` on Container Apps, `COMPUTERNAME` on App Service).

## How it works

- Reads process stats directly from `/proc` — no cgo, no external dependencies.
- Determines the memory limit from cgroups v2 (`memory.max`), falling back to v1 (`memory.limit_in_bytes`). If these are not present, memory utilization will report as zero.
- Emits metrics via OTLP/gRPC to a collector, or to stdout for local runs.

## Configuration

`procmon` is configured entirely through environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `SAMPLE_INTERVAL` | `1m` | Sampling period (Go duration, e.g. `10s`, `30s`, `1m`). |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, or `error`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP/gRPC endpoint. If unset, metrics are printed to stdout. |

## Project layout

```
cmd/                 procmon source (Go)
  main.go            entrypoint: OTel setup + sampling loop
  sysinfo_helper.go  /proc + cgroup sampling logic
  azure_helper.go    Azure environment detection
test-app/            Flask app to generate CPU/memory load for testing
Dockerfile           builds procmon and bundles it with the test app
terraform/           Azure Container Apps deployment (VNet, ACR, LAW, Grafana, Prometheus, OTel Collector)
grafana/             import-ready Grafana dashboard JSON
```

## Build and run

Build the container (compiles `procmon` and bundles the Flask test app):

```sh
docker build -t procmon .
docker run -p 8080:8080 procmon
```

The test app exposes endpoints to drive load:

- `GET /cpu?seconds=10&workers=4` — burn CPU across multiple cores.
- `GET /mem?seconds=10&mb=128` — allocate and hold memory.
- `GET /health` — health check.

To run `procmon` standalone against the host's `/proc`:

```sh
go build -o procmon ./cmd
SAMPLE_INTERVAL=5s LOG_LEVEL=debug ./procmon
```

With no `OTEL_EXPORTER_OTLP_ENDPOINT` set, metrics print to stdout.

## Deploy to Azure

The `terraform/` directory provisions a full observability stack on Azure Container Apps: the procmon app, an OpenTelemetry Collector, Prometheus (remote-write receiver), and Grafana.

```sh
cd terraform
terraform init
terraform apply
```

### Grafana dashboard

Import [grafana/aca-health.json](grafana/aca-health.json) manually through the Grafana UI: **Dashboards → New → Import**, upload the file, and select your Prometheus datasource when prompted. It includes CPU utilization, memory utilization, and RSS panels keyed by `{{instance_id}}:{{name}}({{pid}})`.

## Requirements

- Go 1.26+
- Docker
- Terraform ~> 1.15 and the Azure CLI (for deployment)
