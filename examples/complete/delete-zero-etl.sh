#!/usr/bin/env bash
# Delete the example's zero-ETL integration before Terraform drops its destination database.
set -euo pipefail

integration_arn=$(terraform -chdir="${1:?Supply the Terraform directory}" show -json | jq -r '
  .values.root_module.resources[]? |
  select(.type == "aws_redshift_integration" and .name == "zero_etl") | .values.arn // empty')
if [[ -z "$integration_arn" ]]; then
  exit 0
fi

query="Integrations[?IntegrationArn=='$integration_arn'].IntegrationArn"
remaining=$(aws redshift describe-integrations --query "$query" --output text)
if [[ -n "$remaining" ]]; then
  aws redshift delete-integration --integration-arn "$integration_arn" > /dev/null
fi
for _ in {1..30}; do
  remaining=$(aws redshift describe-integrations --query "$query" --output text)
  if [[ -z "$remaining" ]]; then
    exit 0
  fi
  sleep 10
done
echo 'Zero-ETL integration deletion did not finish within five minutes.' >&2
exit 1
