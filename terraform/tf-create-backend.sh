# !/bin/bash
# This script creates a resource group, storage account, and blob container for storing Terraform state files in Azure.
STATE_RG=""
STATE_LOCATION=""
STATE_ACCOUNT=""
STATE_CONTAINER=""
STATE_KEY=""
GHA_OBJECT_ID=""

az group create \
  --name "$STATE_RG" \
  --location "$STATE_LOCATION"

az storage account create \
  --name "$STATE_ACCOUNT" \
  --resource-group "$STATE_RG" \
  --location "$STATE_LOCATION" \
  --sku Standard_LRS \
  --kind StorageV2 \
  --min-tls-version TLS1_2 \
  --allow-blob-public-access false

az storage container create \
  --name "$STATE_CONTAINER" \
  --account-name "$STATE_ACCOUNT" \
  --auth-mode login

STORAGE_ID=$(az storage account show \
  --name "$STATE_ACCOUNT" \
  --resource-group "$STATE_RG" \
  --query id -o tsv)

az role assignment create \
  --assignee-object-id "$GHA_OBJECT_ID" \
  --assignee-principal-type ServicePrincipal \
  --role "Storage Blob Data Contributor" \
  --scope "$STORAGE_ID"

# Offer to migrate an existing local state file to the remote backend.
if [[ -f "./terraform.tfstate" ]]
then
  if read -r -p "Migrate local Terraform state to the remote backend? [y/N] " MIGRATE_STATE \
    && [[ "$MIGRATE_STATE" =~ ^[Yy]([Ee][Ss])?$ ]]
  then
    terraform init -migrate-state \
      -backend-config="resource_group_name=$STATE_RG" \
      -backend-config="storage_account_name=$STATE_ACCOUNT" \
      -backend-config="container_name=$STATE_CONTAINER" \
      -backend-config="key=$STATE_KEY"
  else
    echo "Skipping Terraform state migration."
  fi
fi