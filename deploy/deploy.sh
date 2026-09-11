#!/usr/bin/env bash
# Deploys the Enceladus API built from the local Nix flake to the production
# host. Refreshes the repository checkout, rebuilds the API and population
# out-links, feeds the runtime environment (including the Redis password) to
# the systemd units and health-checks Quart before promoting the release.
set -Eeuo pipefail

ref=${1:-${SOURCE_REF:-main}}
region=eu-west-1
repo=/opt/enceladus
env_file=/etc/enceladus/runtime.env

export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"

set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

# Redis must exist before the app starts; first bootstrap builds it.
if [[ ! -x $repo/current-redis/bin/redis-server ]]; then
  nix build "$repo#redis" --out-link "$repo/current-redis"
fi
if ! systemctl is-active --quiet enceladus-redis.service; then
  systemctl restart enceladus-redis.service
fi

# Bring the checkout up to the requested ref (deploy.sh is fetched from raw
# GitHub before this runs, so a newer script self-updates the code).
git -C "$repo" fetch --quiet --depth 1 origin "$ref"
git -C "$repo" checkout --quiet --force --detach FETCH_HEAD

previous_password=""
if [[ -f $env_file ]]; then
  previous_password=$(sed -n 's/^REDIS_PASSWORD=//p' "$env_file" | tail -1)
fi

redis_password=$(aws secretsmanager get-secret-value \
  --region "$region" \
  --secret-id "$REDIS_SECRET_ARN" \
  --query SecretString \
  --output text)

umask 077
cat > "$env_file" <<EOF
APP_REF=$ref
AWS_DEFAULT_REGION=$region
AWS_REGION=$region
CORS_ORIGINS=$CORS_ORIGINS
ENCELADUS_DATASUS_CACHE_MAX_BYTES=$ENCELADUS_DATASUS_CACHE_MAX_BYTES
ENCELADUS_DATASUS_CACHE_PATH=$ENCELADUS_DATASUS_CACHE_PATH
ENCELADUS_DATASUS_MAX_YEAR_PATH=$ENCELADUS_DATASUS_MAX_YEAR_PATH
ENCELADUS_HOME=$ENCELADUS_HOME
ENCELADUS_POPULATION_DATA_PATH=$ENCELADUS_POPULATION_DATA_PATH
IBGE_POPULATION_PERIOD=$IBGE_POPULATION_PERIOD
REDIS_HOST=$REDIS_HOST
REDIS_PASSWORD=$redis_password
SES_CONFIGURATION_SET=$SES_CONFIGURATION_SET
SES_SENDER=$SES_SENDER
EOF

if [[ -n $previous_password && $previous_password != "$redis_password" ]]; then
  systemctl restart enceladus-redis.service
fi

nix build "$repo#api" --out-link "$repo/current-api.next"
nix build "$repo#population" --out-link "$repo/current-population.next"

rm -rf "$repo/current-api.previous" "$repo/current-population.previous"
if [[ -e $repo/current-api ]]; then
  mv "$repo/current-api" "$repo/current-api.previous"
fi
mv "$repo/current-api.next" "$repo/current-api"
if [[ -e $repo/current-population ]]; then
  mv "$repo/current-population" "$repo/current-population.previous"
fi
mv "$repo/current-population.next" "$repo/current-population"

systemctl restart enceladus-population.service
systemctl restart enceladus-api.service

if ! curl --fail --retry 30 --retry-delay 5 --retry-connrefused \
  http://127.0.0.1:8000/health; then
  rm -f "$repo/current-api"
  if [[ -e $repo/current-api.previous ]]; then
    mv "$repo/current-api.previous" "$repo/current-api"
  fi
  rm -f "$repo/current-population"
  if [[ -e $repo/current-population.previous ]]; then
    mv "$repo/current-population.previous" "$repo/current-population"
  fi
  systemctl restart enceladus-population.service
  systemctl restart enceladus-api.service
  echo "Deployment failed health checks; previous release restored" >&2
  exit 1
fi

nix-collect-garbage --quiet || true
rm -rf "$repo/current-api.previous" "$repo/current-population.previous"
echo "Deployment healthy: $ref"