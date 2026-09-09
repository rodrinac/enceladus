#!/usr/bin/env bash
set -Eeuo pipefail

image_ref=${1:?Usage: deploy.sh IMAGE_DIGEST_REFERENCE}
region=eu-west-1
repository_root=/opt/enceladus

if [[ ! $image_ref =~ ^[0-9]+\.dkr\.ecr\.eu-west-1\.amazonaws\.com/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]; then
  echo "Refusing invalid image reference: $image_ref" >&2
  exit 2
fi

# shellcheck disable=SC1091 -- this file is provisioned by CloudFormation on the host.
source "$repository_root/runtime.env"

source_base="https://raw.githubusercontent.com/$SOURCE_REPOSITORY/$SOURCE_REF/deploy"
curl --fail --location --retry 5 "$source_base/compose.yml" \
  --output "$repository_root/compose.yml.next"
mv "$repository_root/compose.yml.next" "$repository_root/compose.yml"

previous_image=""
if [[ -f $repository_root/.env ]]; then
  previous_image=$(sed -n 's/^APP_IMAGE=//p' "$repository_root/.env")
fi

redis_password=$(aws secretsmanager get-secret-value \
  --region "$region" \
  --secret-id "$REDIS_SECRET_ARN" \
  --query SecretString \
  --output text)

umask 077
cat > "$repository_root/.env" <<EOF
APP_IMAGE=$image_ref
CORS_ORIGINS=$CORS_ORIGINS
IBGE_POPULATION_PERIOD=$IBGE_POPULATION_PERIOD
REDIS_PASSWORD=$redis_password
SES_CONFIGURATION_SET=$SES_CONFIGURATION_SET
SES_SENDER=$SES_SENDER
EOF

registry=${image_ref%%/*}
aws ecr get-login-password --region "$region" \
  | docker login --username AWS --password-stdin "$registry"

cd "$repository_root"
docker compose pull
docker compose up --detach --remove-orphans

if ! curl --fail --retry 12 --retry-delay 5 --retry-connrefused \
  http://127.0.0.1:8000/health; then
  if [[ -n $previous_image && $previous_image != "$image_ref" ]]; then
    sed -i "s|^APP_IMAGE=.*|APP_IMAGE=$previous_image|" "$repository_root/.env"
    docker compose pull
    docker compose up --detach --remove-orphans
  fi
  echo "Deployment failed health checks; previous image restored" >&2
  exit 1
fi

docker image prune --force --filter "until=168h"
curl --fail --location --retry 5 "$source_base/deploy.sh" \
  --output "$repository_root/deploy.sh.next"
chmod 0755 "$repository_root/deploy.sh.next"
mv "$repository_root/deploy.sh.next" "$repository_root/deploy.sh"
echo "Deployment healthy: $image_ref"
