#!/usr/bin/env bash
# One-time EC2 bootstrap: installs Nix, mounts the data volume, configures the
# systemd units and performs the initial deploy. Runs from cloud-init user-data
# after the repository has been cloned to /opt/enceladus, and is safe to re-run
# manually on an existing (Docker-era) host to migrate it to the Nix runtime.
#
# Emits GitHub Actions workflow commands so progress groups render in job logs.
set -Eeuo pipefail
export HOME="${HOME:-/root}"

repo=/opt/enceladus
legacy_repo=/opt/enceladus.legacy
env_file=/etc/enceladus/runtime.env

echo "::group::Repository"
# A loose Docker-era /opt/enceladus (no git work tree) gets a real checkout so
# the flake, scripts and this bootstrap can run from it. Stacks created by the
# current user data already clone the repository here.
if [[ ! -d $repo/.git ]]; then
  if [[ -d $repo ]]; then
    mv "$repo" "$legacy_repo"
  fi
  if ! command -v git >/dev/null 2>&1; then
    dnf install --assumeyes git
  fi
  git clone --depth 1 --branch "${SOURCE_REF:-main}" \
    "https://github.com/${SOURCE_REPOSITORY:-rodrinac/enceladus}" "$repo"
fi
echo "::endgroup::"

echo "::group::Runtime environment"
mkdir --parents /etc/enceladus
# Keep the environment CloudFormation originally injected into a Docker-era
# host so the Nix runtime adopts the same settings.
if [[ ! -f $repo/runtime.env && -f $legacy_repo/runtime.env ]]; then
  cp "$legacy_repo/runtime.env" "$repo/runtime.env"
fi
if [[ ! -f $env_file ]]; then
  echo "Missing $env_file; synthesizing it (adopting legacy values when present)"
  legacy_env=/opt/enceladus/runtime.env
  if [[ -f $legacy_env ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$legacy_env"
    set +a
  fi
  umask 077
  cat > "$env_file" <<EOF
CORS_ORIGINS=${CORS_ORIGINS:?CORS_ORIGINS must be set in legacy runtime.env}
ENCELADUS_DATASUS_CACHE_MAX_BYTES=${ENCELADUS_DATASUS_CACHE_MAX_BYTES:-5368709120}
ENCELADUS_DATASUS_CACHE_PATH=${ENCELADUS_DATASUS_CACHE_PATH:-/srv/enceladus/relatorios/.cache/datasus}
ENCELADUS_DATASUS_MAX_YEAR_PATH=${ENCELADUS_DATASUS_MAX_YEAR_PATH:-/srv/enceladus/population/datasus-max-year.txt}
ENCELADUS_HOME=${ENCELADUS_HOME:-/srv/enceladus}
ENCELADUS_POPULATION_DATA_PATH=${ENCELADUS_POPULATION_DATA_PATH:-/srv/enceladus/population/population.csv}
IBGE_POPULATION_PERIOD=${IBGE_POPULATION_PERIOD:-last%201}
REDIS_HOST=${REDIS_HOST:-127.0.0.1}
REDIS_SECRET_ARN=${REDIS_SECRET_ARN:?REDIS_SECRET_ARN must be set in legacy runtime.env}
SES_CONFIGURATION_SET=${SES_CONFIGURATION_SET:-}
SES_SENDER=${SES_SENDER:?SES_SENDER must be set in legacy runtime.env}
SOURCE_REF=${SOURCE_REF:-main}
SOURCE_REPOSITORY=${SOURCE_REPOSITORY:-rodrinac/enceladus}
EOF
fi
set -a
# shellcheck disable=SC1090
source "$env_file"
set +a
echo "::endgroup::"

export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"

echo "::group::Mount data volume"
# Mount the retained EBS data volume under /srv/enceladus (idempotent).
device=""
for _ in $(seq 1 60); do
  root_partition=$(findmnt -n -o SOURCE /)
  root_disk=$(lsblk -n -o PKNAME "$root_partition")
  device=$(lsblk -dn -o NAME,TYPE | awk -v root="$root_disk" '$2 == "disk" && $1 != root {print "/dev/"$1; exit}')
  if [[ -n $device ]]; then break; fi
  sleep 2
done
[[ -n $device ]]

if ! blkid "$device" >/dev/null 2>&1; then
  mkfs.xfs "$device"
fi
uuid=$(blkid -s UUID -o value "$device")
if ! grep -q "$uuid" /etc/fstab; then
  echo "UUID=$uuid /srv/enceladus xfs defaults,nofail 0 2" >>/etc/fstab
fi
mkdir --parents /srv/enceladus
findmnt /srv/enceladus >/dev/null 2>&1 || mount /srv/enceladus
mkdir --parents /srv/enceladus/population /srv/enceladus/redis
chmod 0777 /srv/enceladus/population /srv/enceladus/redis
echo "Data volume mounted at /srv/enceladus"
echo "::endgroup::"

echo "::group::Install Nix"
# Install Nix with a systemd-managed daemon (multi-user).
if [[ ! -x /nix/var/nix/profiles/default/bin/nix ]]; then
  curl --fail --location --retry 5 https://nixos.org/nix/install --output /tmp/nix-install
  bash /tmp/nix-install --daemon
  export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"
fi
nix --version
# Flake builds need the nix-command and flakes experimental features; the
# stock installer's nix.conf does not enable them.
if ! grep -q 'experimental-features' /etc/nix/nix.conf 2>/dev/null; then
  echo 'experimental-features = nix-command flakes' >>/etc/nix/nix.conf
fi
echo "::endgroup::"

# Migrating an existing Docker-era host: stop the legacy Compose stack so it
# never competes with systemd for port 8000 or the data volume, and prune
# images to make room for the Nix store on small root volumes.
if command -v docker >/dev/null 2>&1; then
  echo "::group::Stop legacy Docker services"
  for dir in "$repo" "$legacy_repo"; do
    if [[ -f $dir/compose.yml ]]; then
      (cd "$dir" && docker compose down --timeout 30) || true
    fi
  done
  docker system prune --all --force --volumes 2>/dev/null || true
  systemctl disable --now docker.service containerd.service 2>/dev/null || true
  echo "Legacy Compose stack stopped and disabled"

  # Old Stack served PDFs from /srv/enceladus/reports; the Nix runtime reads
  # $ENCELADUS_HOME/relatorios (default /srv/enceladus/relatorios). Preserve
  # the existing reports so history keeps listing after the migration.
  if [[ -d /srv/enceladus/reports && ! -e /srv/enceladus/relatorios ]]; then
    mv /srv/enceladus/reports /srv/enceladus/relatorios
    echo "Moved legacy reports to /srv/enceladus/relatorios"
  fi
  echo "::endgroup::"
fi

echo "::group::Provision systemd units"
# Build Redis once so the systemd unit has a binary to exec.
if [[ ! -x $repo/current-redis/bin/redis-server ]]; then
  nix build "$repo#redis" --out-link "$repo/current-redis"
fi

cat >/etc/systemd/system/enceladus-redis.service <<'UNIT'
[Unit]
Description=Enceladus Redis
After=local-fs.target network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/enceladus/runtime.env
ExecStart=/bin/sh -c 'exec /opt/enceladus/current-redis/bin/redis-server --requirepass "$REDIS_PASSWORD" --appendonly yes --appendfsync everysec --dir /srv/enceladus/redis --port 6379'
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ReadWritePaths=/srv/enceladus/redis

[Install]
WantedBy=multi-user.target
UNIT

cat >/etc/systemd/system/enceladus-population.service <<'UNIT'
[Unit]
Description=Enceladus IBGE population data
After=local-fs.target network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/etc/enceladus/runtime.env
ExecStart=/bin/sh -c 'exec /opt/enceladus/current-population/bin/enceladus-population'
TimeoutStartSec=600
Restart=no
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT

cat >/etc/systemd/system/enceladus-population.timer <<'UNIT'
[Unit]
Description=Refresh Enceladus IBGE population data monthly

[Timer]
OnCalendar=*-*-01 04:00:00
Persistent=true
Unit=enceladus-population.service

[Install]
WantedBy=timers.target
UNIT

cat >/etc/systemd/system/enceladus-api.service <<'UNIT'
[Unit]
Description=Enceladus Quart API
After=network-online.target enceladus-redis.service
Wants=network-online.target enceladus-redis.service

[Service]
Type=simple
EnvironmentFile=/etc/enceladus/runtime.env
ExecStart=/bin/sh -c 'exec /opt/enceladus/current-api/bin/enceladus-api'
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ReadWritePaths=/srv/enceladus

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable enceladus-redis.service enceladus-population.service enceladus-population.timer enceladus-api.service
echo "Units written and enabled"
echo "::endgroup::"

echo "::group::Initial deploy"
# Build the app and populate data, then start every service.
bash "$repo/deploy/deploy.sh" "$SOURCE_REF"
echo "::endgroup::"