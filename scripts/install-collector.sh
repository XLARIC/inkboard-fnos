#!/usr/bin/env bash
# InkBoard fnOS collector installer. Read before running as administrator.
# No hub address, pairing credential, or administrator password is embedded.
set -euo pipefail
umask 077
release=0.1.0-alpha.1
base="https://github.com/XLARIC/inkboard-fnos/releases/download/v$release"
[ "$(id -u)" = 0 ] || { echo "请查看脚本后使用 sudo bash collector.sh 执行。"; exit 1; }
[ -t 0 ] || { echo "需要交互式终端输入配置，不能直接管道执行。"; exit 1; }
command -v appcenter-cli >/dev/null || { echo "仅支持带 appcenter-cli 的飞牛 fnOS。"; exit 1; }
[ ! -d /var/apps/inkboard-fnos ] || { echo "已有 InkBoard，请在应用中心管理现有应用；未覆盖配置。"; exit 1; }
case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo "暂不支持此架构";exit 1;; esac
for tool in curl sha256sum;do command -v "$tool" >/dev/null || { echo "缺少工具：$tool";exit 1; };done
echo "将安装原生采集器：普通用户提供 HTTPS 指标，root 助手读取系统统计。"
echo "不添加到任何主端，不开放公网，不修改驱动。"
read -r -p "确认安装预览版（输入 INSTALL）：" confirmation
[ "$confirmation" = INSTALL ] || exit 1
read -r -p "监听 IP [0.0.0.0]：" address;address=${address:-0.0.0.0}
read -r -p "管理页面端口 [18888]，指标接口使用下一端口：" port;port=${port:-18888}
[[ "$address" =~ ^[0-9a-fA-F.:]+$ ]] || { echo "请输入 IP 地址";exit 1; }
[[ "$port" =~ ^[0-9]{4,5}$ ]] || exit 1
[ "$((10#$port))" -ge 1024 ] && [ "$((10#$port))" -le 65534 ] || exit 1
read -r -s -p "设置管理密码（至少 12 字节）：" password;echo
read -r -s -p "再次输入：" repeated;echo
[ "$password" = "$repeated" ] && [ "${#password}" -ge 12 ] || { echo "密码不一致或过短";exit 1; }
workdir=$(mktemp -d)
trap 'unset password repeated;rm -rf "$workdir"' EXIT
file="inkboard-fnos-$release-$arch.fpk"
curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error "$base/$file" -o "$workdir/$file"
curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error "$base/SHA256SUMS" -o "$workdir/SHA256SUMS"
checksum=$(awk -v f="$file" '$2==f{print $1}' "$workdir/SHA256SUMS")
[[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || { echo "发行校验文件无效";exit 1; }
printf '%s  %s\n' "$checksum" "$file" >"$workdir/check"
(cd "$workdir";sha256sum -c check)
printf 'wizard_role=agent\nwizard_listen=%s\nwizard_port=%s\nwizard_password=%s\n' "$address" "$port" "$password" >"$workdir/config.env"
unset password repeated
appcenter-cli install-fpk "$workdir/$file" --env "$workdir/config.env"
echo "安装完成后打开 http://这台NAS的局域网IP:$port/admin。"
echo "登录并生成配对码，在主端电脑网页的「添加远端 NAS」中验证添加。"
