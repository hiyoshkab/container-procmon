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

### Automated deployments and rollback

The [Build workflow](.github/workflows/build.yaml) builds and pushes an image to
Docker Hub when application, build, or Terraform files change on `main`. It calls
the reusable [Terraform workflow](.github/workflows/terraform.yaml) with the pushed
image's exact digest. Terraform plans automatically; apply waits for approval if
required reviewers are configured on the GitHub `production` environment. Configure
those reviewers before enabling this pipeline if deployments must be approved.

Configure the `DOCKERHUB_REPO` repository variable (the namespace before `/procmon`)
and the `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` secrets. The image must be publicly
pullable with the current Terraform configuration; private repositories require
Container Apps registry authentication.

The apply job records the image digest in its summary and checks the application's
`/health` endpoint. A failed check fails the deployment job but does not automatically
roll back an already applied image. Verify telemetry separately. The health check
requires the application URL to be reachable from the GitHub-hosted runner.

To roll back:

1. Copy the image reference from a previous successful apply job's summary.
2. Run **Actions > Terraform > Run workflow** on `main`.
3. Set `container_image` to that `repository@sha256:digest` reference and enable
   `apply`. Leave `apply` disabled to generate a plan only.
4. Review and approve the new plan, then verify the health check and telemetry.

Rollback changes the application image using the current Terraform configuration;
it does not restore old infrastructure, secrets, or persistent data. Retain
previously deployed images in Docker Hub. Manual runs always require an explicit
digest, including infrastructure-only deployments, so they cannot silently use a
stale image variable. To change infrastructure without changing the image, use
the currently deployed digest in a manual Terraform run.

Build workflow dispatches outside `main` only build the image; Terraform deployments
run only on `main`. Production Terraform runs are serialized.

### GitHub Actions Terraform variables

The [Terraform workflow](.github/workflows/terraform.yaml) forwards these optional
GitHub repository variables to Terraform:

- `RESOURCE_GROUP_NAME`, `LOCATION`, `NAME_PREFIX`

Other optional inputs in [terraform/variables.tf](terraform/variables.tf) use their
declared defaults and can be configured locally or explicitly added to the plan
step's environment.
`container_image` comes from the required workflow input, not a `CONTAINER_IMAGE`
repository variable.

Unset or empty forwarded variables use Terraform's declared defaults.

The workflow also requires the `TF_STATE_RESOURCE_GROUP`, `TF_STATE_STORAGE_ACCOUNT`,
`TF_STATE_CONTAINER`, and `TF_STATE_KEY` repository variables for its remote backend.
Apply uses the saved plan, so Terraform input variables are only supplied during
planning.

### GitHub Actions Azure authentication

The workflow uses OpenID Connect (OIDC) for Azure Login and Terraform, with no
client secret. Configure these GitHub repository secrets:

- `AZURE_CLIENT_ID`: application/client ID of a Microsoft Entra application or
  user-assigned managed identity.
- `AZURE_TENANT_ID`: Microsoft Entra tenant ID.
- `AZURE_SUBSCRIPTION_ID`: target Azure subscription ID.

On that application or managed identity, create two federated identity credentials
with issuer `https://token.actions.githubusercontent.com`, audience
`api://AzureADTokenExchange`, and these subjects:

| Job | Subject |
| --- | --- |
| Plan on `main` | `repo:hiyoshkab/container-procmon:ref:refs/heads/main` |
| Apply in `production` | `repo:hiyoshkab/container-procmon:environment:production` |

Run manual workflows from `main` to match the plan credential. Configure approval
and deployment branch restrictions for the GitHub `production` environment.
Grant the identity appropriate deployment permissions and **Storage Blob Data
Contributor** on the state container. If the Terraform configuration manages role
assignments, it also needs permission to create those assignments.

Once OIDC is configured and verified, remove the unused `AZURE_CREDENTIALS` GitHub
secret and revoke its old client secret if no other workload uses it. See
[Microsoft's OIDC setup guide](https://learn.microsoft.com/en-us/azure/developer/github/connect-from-azure-openid-connect)
and [Terraform's Azure backend authentication guide](https://developer.hashicorp.com/terraform/language/backend/azurerm).

### Grafana dashboard

Import [grafana/aca-health.json](grafana/aca-health.json) manually through the Grafana UI: **Dashboards → New → Import**, upload the file, and select your Prometheus datasource when prompted. It includes CPU utilization, memory utilization, and RSS panels keyed by `{{instance_id}}:{{name}}({{pid}})`.

## Requirements

- Go 1.26+
- Docker
- Terraform ~> 1.15 and the Azure CLI (for deployment)
