package main

import (
	"os"
)

// Detect whether it's an App Service or Container Apps.
func getAzureEnvironmentInfo() (string, string) {
	envName := ""
	instanceId := ""
	if os.Getenv("WEBSITE_SITE_NAME") != "" && os.Getenv("COMPUTERNAME") != "" {
		envName = "App Service Linux"
		instanceId = os.Getenv("COMPUTERNAME")
	} else if os.Getenv("CONTAINER_APP_NAME") != "" && os.Getenv("CONTAINER_APP_REPLICA_NAME") != "" {
		envName = "Azure Container Apps"
		instanceId = os.Getenv("CONTAINER_APP_REPLICA_NAME")
	} else {
		envName = "Unknown"
		instanceId = "Unknown"
	}
	return envName, instanceId
}
