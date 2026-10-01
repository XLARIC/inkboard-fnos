#!/bin/bash
set -euo pipefail
# Run inside an isolated Debian container as root, never on a real fnOS host.
[ -f /.dockerenv ] || { echo "仅在隔离的测试容器内运行";exit 1; }
root=$(mktemp -d)
trap '[ ! -x "$root/cmd/main" ] || "$root/cmd/main" stop;rm -rf "$root"' EXIT
cp -R /source/packaging/fnos/. "$root/"
mkdir -p "$root/app/bin" "$root/etc" "$root/var"
arch=$(uname -m);case "$arch" in aarch64) arch=arm64;;x86_64)arch=amd64;;esac
cp "/source/dist/inkboard-linux-$arch" "$root/app/bin/inkboard"
chmod +x "$root"/cmd/* "$root/app/bin/inkboard"
groupadd inkboard-fnos;useradd -M -g inkboard-fnos -s /usr/sbin/nologin inkboard-fnos
chmod 755 "$root";chown inkboard-fnos:inkboard-fnos "$root/etc" "$root/var"
export TRIM_APPDEST="$root/app" TRIM_PKGETC="$root/etc" TRIM_PKGVAR="$root/var" TRIM_USERNAME=inkboard-fnos TRIM_TEMP_LOGFILE="$root/error.log"
export wizard_role=hub wizard_listen=127.0.0.1 wizard_port=18998 wizard_password=container-test-password
"$root/cmd/install_init"
"$root/cmd/install_callback"
unset wizard_password
grep -q '"monitor_local": false' "$root/etc/config.json"
runuser -u inkboard-fnos -- "$root/app/bin/inkboard" init -config-dir "$root/etc" -data-dir "$root/var/cache" -monitor-local
"$root/cmd/main" start
"$root/cmd/main" status
wrapper=$(cat /run/inkboard-fnos/web.pid)
child=$(cat "/proc/$wrapper/task/$wrapper/children");child="${child%% *}"
uid=$(awk '/^Uid:/{print $2}' "/proc/$child/status")
[ "$uid" = "$(id -u inkboard-fnos)" ]
[ "$(stat -c %a /run/inkboard-fnos/snapshot.json)" = 640 ]
[ "$(stat -c %G /run/inkboard-fnos/snapshot.json)" = inkboard-fnos ]
runuser -u inkboard-fnos -- test -r /run/inkboard-fnos/snapshot.json
if runuser -u inkboard-fnos -- test -w /run/inkboard-fnos/snapshot.json;then exit 1;fi
"$root/cmd/config_init"
export wizard_role=agent
"$root/cmd/config_callback"
grep -q '"role": "agent"' "$root/etc/config.json"
"$root/cmd/upgrade_init"
[ -n "$(ls "$root/var/backups"/config-*.tar.gz)" ]
"$root/cmd/upgrade_callback"
grep -q '18998' "$root/app/ui/config"
"$root/cmd/uninstall_init"
"$root/cmd/uninstall_callback"
[ -s "$root/etc/config.json" ] && [ -s "$root/etc/master.key" ]
status=0;"$root/cmd/main" status || status=$?;[ "$status" = 3 ]
echo "PASS: install/start/status, unprivileged web, read-only helper snapshot, config role switch, upgrade backup/entry, uninstall preserves files."
