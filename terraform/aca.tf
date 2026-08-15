resource "azurerm_virtual_network" "this" {
  name                = "${var.name_prefix}-vnet"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  address_space       = var.vnet_address_space
  tags                = var.tags
}

resource "azurerm_subnet" "aca_infra" {
  name                 = "${var.name_prefix}-aca-infra"
  resource_group_name  = azurerm_resource_group.rg.name
  virtual_network_name = azurerm_virtual_network.this.name
  address_prefixes     = var.aca_infra_subnet_prefixes

  delegation {
    name = "aca"
    service_delegation {
      name    = "Microsoft.App/environments"
      actions = ["Microsoft.Network/virtualNetworks/subnets/join/action"]
    }
  }
}

# Log Analytics for ACA
resource "azurerm_log_analytics_workspace" "aca-law" {
  name                = "${var.name_prefix}-law"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
}

resource "azurerm_container_app_environment" "cae" {
  name                       = "${var.name_prefix}-env"
  resource_group_name        = azurerm_resource_group.rg.name
  location                   = azurerm_resource_group.rg.location
  logs_destination           = "azure-monitor"

  infrastructure_subnet_id       = azurerm_subnet.aca_infra.id
  internal_load_balancer_enabled = var.aca_internal_only
}

resource "azurerm_monitor_diagnostic_setting" "cae-diag" {
  name                       = "${var.name_prefix}-env-diag"
  target_resource_id         = azurerm_container_app_environment.cae.id
  log_analytics_workspace_id = azurerm_log_analytics_workspace.aca-law.id

  enabled_log {
    category = "ContainerAppConsoleLogs"
  }

  enabled_log {
    category = "ContainerAppSystemLogs"
  }
}

resource "azurerm_container_app" "main-app" {
  name                         = "${var.name_prefix}-app"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.cae.id
  revision_mode                = "Single"

  template {
    min_replicas = 1
    max_replicas = 1

    container {
      name   = "procmon"
      image  = var.container_image
      cpu    = var.container_cpu
      memory = var.container_memory

      env {
        name  = "SAMPLE_INTERVAL"
        value = var.sample_interval
      }

      env {
        name  = "LOG_LEVEL"
        value = var.log_level
      }

      env {
        name  = "OTEL_EXPORTER_OTLP_ENDPOINT"
        value = var.otel_exporter_otlp_endpoint
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 8080
    transport        = "auto"

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }
}

# Grafana dashboards — publicly reachable UI.
resource "azurerm_container_app" "grafana" {
  name                         = "grafana"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.cae.id
  revision_mode                = "Single"

  template {
    min_replicas = 1
    max_replicas = 1

    container {
      name   = "grafana"
      image  = "grafana/grafana:11.3.0"
      cpu    = 0.5
      memory = "1Gi"

      env {
        name  = "GF_AUTH_AZURE_AUTH_ENABLED"
        value = "true"
      }

      env {
        name  = "GF_AZURE_CLOUD"
        value = "AzureCloud"
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 3000
    transport        = "auto"

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }
}

# Prometheus — internal-only, scraped by Grafana / receives remote-write.
resource "azurerm_container_app" "prometheus" {
  name                         = "prometheus"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.cae.id
  revision_mode                = "Single"

  template {
    min_replicas = 1
    max_replicas = 1

    volume {
      name         = "prom-data"
      storage_type = "EmptyDir"
    }

    container {
      name    = "prometheus"
      image   = "prom/prometheus:v3.1.0"
      cpu     = 1.0
      memory  = "2Gi"
      command = ["/bin/prometheus"]
      args = [
        "--config.file=/etc/prometheus/prometheus.yml",
        "--storage.tsdb.path=/prometheus",
        "--storage.tsdb.retention.time=7d",
        "--web.enable-remote-write-receiver",
        "--web.enable-lifecycle",
      ]

      volume_mounts {
        name = "prom-data"
        path = "/prometheus"
      }
    }
  }

  ingress {
    external_enabled = false
    target_port      = 9090
    transport        = "auto"

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }
}

# OpenTelemetry Collector — internal OTLP endpoint (gRPC/HTTP2).
resource "azurerm_container_app" "otel_collector" {
  name                         = "otel-collector-contrib"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.cae.id
  revision_mode                = "Single"

  # Collector config YAML, mounted as a file via a Secret volume.
  # Falls back to the bundled otel-config.yaml when var.otel_config is unset.
  secret {
    name  = "otel-config"
    value = var.otel_config != "" ? var.otel_config : file("${path.module}/otel-config.yaml")
  }

  template {
    min_replicas = 1
    max_replicas = 1

    volume {
      name         = "otel-config"
      storage_type = "Secret"
    }

    container {
      name   = "otel-collector-contrib"
      image  = "otel/opentelemetry-collector-contrib:latest"
      cpu    = 0.5
      memory = "1Gi"
      args   = ["--config", "/etc/otelcol/otel-config"]

      volume_mounts {
        name = "otel-config"
        path = "/etc/otelcol"
      }
    }
  }

  ingress {
    external_enabled           = false
    target_port                = 4317
    transport                  = "http2"
    allow_insecure_connections = true

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }
}