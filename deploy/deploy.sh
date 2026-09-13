#!/usr/bin/env bash
# Deploys the Enceladus API built from the local Nix flake to the production
# host. Refreshes the repository checkout, rebuilds the API and population
# out-links, feeds the runtime environment (including the Redis password) to
# the systemd units and health-checks the API before promoting the release.
#
# Emits GitHub Actions workflow commands so the phases render as collapsible
# groups and failures appear prominently in the Deploy API to EC2 job log.
set -Eeuo pipefail
export HOME="${HOME:-/root}"

ref=${1:-${SOURCE_REF:-main}}
region=eu-west-1
repo=/opt/enceladus
env_file=/etc/enceladus/runtime.env

export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"

echo "::group::Prepare release"
echo "Deploying Enceladus reference: $ref"
if [[ ! -f $env_file ]]; then
  echo "::error file=deploy/deploy.sh::Missing $env_file so this host was never bootstrapped with Nix. Run: sudo bash /opt/enceladus/deploy/bootstrap.sh" >&2
  exit 2
fi
if ! command -v nix >/dev/null 2>&1; then
  echo "::error file=deploy/deploy.sh::Nix is not installed. Run: sudo bash /opt/enceladus/deploy/bootstrap.sh" >&2
  exit 2
fi
set -a
# shellcheck disable=SC1090
source "$env_file"
set +a
echo "::endgroup::"

# Reference the live release out-links from the Nix GC roots directory so the
# nix-collect-garbage step below can never delete the closures of the running
# releases (deleting them leaves dangling current-* symlinks and every later
# restart fails with exit status 127).
pin_current_release() {
  local gcroots=/nix/var/nix/gcroots/enceladus
  mkdir -p "$gcroots"
  local unit
  for unit in api population redis; do
    local link=$repo/current-$unit
    if [[ -e $link ]]; then
      ln -sfn "$(readlink -f "$link")" "$gcroots/$unit"
    fi
  done
}

echo "::group::Sync repository"
git -C "$repo" fetch --quiet --depth 1 origin "$ref"
git -C "$repo" checkout --quiet --force --detach FETCH_HEAD
git -C "$repo" log -1 --oneline
echo "::endgroup::"

echo "::group::Secrets and runtime environment"
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
HOME=${HOME:-/root}
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
REDIS_SECRET_ARN=$REDIS_SECRET_ARN
# Defaulted (not :?) so hosts bootstrapped before the S3 report store existed
# — whose runtime.env has no such lines — deploy instead of dying on an
# unbound variable under `set -u`. Empty means local-filesystem storage.
ENCELADUS_REPORTS_BUCKET=${ENCELADUS_REPORTS_BUCKET:-}
ENCELADUS_REPORTS_PREFIX=${ENCELADUS_REPORTS_PREFIX:-}
SOURCE_REF=$SOURCE_REF
SOURCE_REPOSITORY=$SOURCE_REPOSITORY
EOF
echo "Redis password refreshed from Secrets Manager"
echo "::endgroup::"

echo "::group::Build releases"
# Redis must exist before the app starts; the first bootstrap builds it.
if [[ ! -x $repo/current-redis/bin/redis-server ]]; then
  nix build "$repo#redis" --out-link "$repo/current-redis"
fi
if [[ -n $previous_password && $previous_password != "$redis_password" ]]; then
  echo "::warning::Redis password rotated; restarting enceladus-redis"
  systemctl restart enceladus-redis.service
elif ! systemctl is-active --quiet enceladus-redis.service; then
  systemctl start enceladus-redis.service
fi

nix build "$repo#api" --out-link "$repo/current-api.next"
nix build "$repo#population" --out-link "$repo/current-population.next"
echo "::endgroup::"

echo "::group::Restart services"
rm -rf "$repo/current-api.previous" "$repo/current-population.previous"
if [[ -e $repo/current-api ]]; then
  mv "$repo/current-api" "$repo/current-api.previous"
fi
mv "$repo/current-api.next" "$repo/current-api"
if [[ -e $repo/current-population ]]; then
  mv "$repo/current-population" "$repo/current-population.previous"
fi
mv "$repo/current-population.next" "$repo/current-population"

pin_current_release

systemctl restart enceladus-population.service
systemctl restart enceladus-api.service
echo "::endgroup::"

echo "::group::Health check"
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
  pin_current_release
  echo "::endgroup::"
  echo "::error file=deploy/deploy.sh::Deployment failed health checks; previous release restored"
  exit 1
fi
echo "::endgroup::"

echo "::group::Clean up"
nix-collect-garbage --quiet || true
rm -rf "$repo/current-api.previous" "$repo/current-population.previous"
echo "::endgroup::"

echo "Deployment healthy: $ref"