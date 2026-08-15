output "resource_group_name" {
  description = "Name of the created resource group."
  value       = azurerm_resource_group.rg.name
}

output "container_app_url" {
  description = "Public URL of the container app."
  value       = "https://${azurerm_container_app.main-app.ingress[0].fqdn}"
}

output "grafana_url" {
  description = "Public URL of the Grafana dashboard."
  value       = "https://${azurerm_container_app.grafana.ingress[0].fqdn}"
}