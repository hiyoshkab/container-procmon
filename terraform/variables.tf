variable "resource_group_name" {
  type        = string
  description = "Name of the resource group to create."
  default     = "container-procmon-rg"
}

variable "location" {
  type        = string
  description = "Azure region for all resources."
  default     = "centralus"
}

variable "name_prefix" {
  type        = string
  description = "Prefix applied to resource names."
  default     = "procmon"
}

variable "container_image" {
  type        = string
  description = "Fully qualified container image, e.g. <acr>.azurecr.io/container-procmon:latest."
  default     = "docker.io/hiyoshkab/procmon:v3"
}

variable "container_cpu" {
  type        = number
  description = "vCPU allocated to the container."
  default     = 0.5
}

variable "container_memory" {
  type        = string
  description = "Memory allocated to the container (e.g. \"1Gi\")."
  default     = "1Gi"
}

variable "sample_interval" {
  type        = string
  description = "procmon SAMPLE_INTERVAL env value."
  default     = "10s"
}

variable "log_level" {
  type        = string
  description = "procmon LOG_LEVEL env value."
  default     = "info"
}

variable "otel_exporter_otlp_endpoint" {
  type        = string
  description = "OTLP gRPC endpoint; include scheme, e.g. http://otel-collector-contrib:80."
  default     = "http://otel-collector-contrib:80"
}

variable "vnet_address_space" {
  type        = list(string)
  description = "Address space for the virtual network."
  default     = ["10.0.0.0/16"]
}

variable "aca_infra_subnet_prefixes" {
  type        = list(string)
  description = "Address prefixes for the Container Apps infrastructure subnet."
  default     = ["10.0.0.0/23"]
}

variable "aca_internal_only" {
  type        = bool
  description = "If true, the Container Apps environment uses an internal load balancer (no public ingress endpoint)."
  default     = false
}

variable "otel_config" {
  type        = string
  description = "OpenTelemetry Collector config YAML. Defaults to the bundled otel-config.yaml when left empty."
  default     = ""
}

variable "tags" {
  type        = map(string)
  description = "Tags applied to all resources."
  default     = {}
}
