#!/usr/bin/env bash
# One-time EC2 bootstrap: installs Nix, mounts the data volume, configures the
# systemd units and performs the initial deploy. Runs from cloud-init user-data
# after the repository has been cloned to /opt/enceladus.
set -Eeuo pipefail

repo=/opt/enceladus
env_file=/etc/enceladus/runtime.env

set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"

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

# Install Nix with a systemd-managed daemon (multi-user).
if [[ ! -x /nix/var/nix/profiles/default/bin/nix ]]; then
  curl --fail --location --retry 5 https://nixos.org/nix/install --output /tmp/nix-install
  bash /tmp/nix-install --daemon
fi
export PATH="/nix/var/nix/profiles/default/bin:/root/.nix-profile/bin:$PATH"

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
systemctl enable enceladus-redis.service enceladus-population.service enceladus-api.service

# Build the app and populate data, then start every service.
bash "$repo/deploy/deploy.sh" "$SOURCE_REF"