#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/runq-installer-test.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

release_dir="${tmp_dir}/releases/vtest"
mkdir -p "$release_dir"

write_checksum() {
  file=$1
  output=$2
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" > "$output"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" > "$output"
  else
    printf 'sha256sum or shasum is required for installer tests\n' >&2
    exit 1
  fi
}

make_asset() {
  name=$1
  marker=$2
  cat > "${release_dir}/${name}" <<EOF
#!/bin/sh
printf '%s\\n' '${marker}'
EOF
  chmod +x "${release_dir}/${name}"
  write_checksum "${release_dir}/${name}" "${release_dir}/${name}.sha256"
}

make_asset runq-linux-amd64 core-linux
make_asset runq-dashboard-linux-amd64 ui-linux
make_asset runq-darwin-arm64 core-darwin
make_asset runq-dashboard-darwin-arm64 ui-darwin

api_dir="${tmp_dir}/api/releases"
mkdir -p "$api_dir"
printf '%s\n' '{"tag_name":"vtest"}' > "${api_dir}/latest"

# The default path resolves the stable latest-release endpoint, installs only
# the core client, and has no daemon/data-directory side effects.
linux_bin="${tmp_dir}/linux-bin"
side_effect_dir="${tmp_dir}/must-not-exist"
unset RUNQ_VERSION RUNQ_START_DAEMON
RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
RUNQ_API_BASE="file://${tmp_dir}/api" \
RUNQ_OS=linux \
RUNQ_ARCH=amd64 \
RUNQ_INSTALL_DIR="$linux_bin" \
RUNQ_WITH_UI=0 \
RUNQ_DATA_DIR="$side_effect_dir" \
  sh "${repo_root}/install.sh" > "${tmp_dir}/default-install.log"

[ "$("${linux_bin}/runq")" = "core-linux" ]
[ ! -e "${linux_bin}/runqd" ]
[ ! -e "$side_effect_dir" ]
if grep -E 'Starting|Restarting' "${tmp_dir}/default-install.log" >/dev/null 2>&1; then
  printf 'default install unexpectedly started or restarted a daemon\n' >&2
  exit 1
fi

# Explicit versions may omit the v prefix, and the UI selection installs the
# dashboard build under the same runq command name.
darwin_bin="${tmp_dir}/darwin-bin"
RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
RUNQ_VERSION=test \
RUNQ_OS=darwin \
RUNQ_ARCH=arm64 \
RUNQ_INSTALL_DIR="$darwin_bin" \
RUNQ_WITH_UI=1 \
  sh "${repo_root}/install.sh"

[ "$("${darwin_bin}/runq")" = "ui-darwin" ]
[ ! -e "${darwin_bin}/runqd" ]

# A failed checksum must leave an existing installation untouched.
bad_release_dir="${tmp_dir}/releases/vbad"
mkdir -p "$bad_release_dir"
cp "${release_dir}/runq-linux-amd64" "${bad_release_dir}/runq-linux-amd64"
printf '%s  %s\n' '0000000000000000000000000000000000000000000000000000000000000000' 'runq-linux-amd64' \
  > "${bad_release_dir}/runq-linux-amd64.sha256"
bad_bin="${tmp_dir}/bad-bin"
mkdir -p "$bad_bin"
printf '%s\n' 'old-client' > "${bad_bin}/runq"
if RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
  RUNQ_VERSION=vbad \
  RUNQ_OS=linux \
  RUNQ_ARCH=amd64 \
  RUNQ_INSTALL_DIR="$bad_bin" \
  RUNQ_WITH_UI=0 \
    sh "${repo_root}/install.sh" >/dev/null 2>&1; then
  printf 'expected checksum mismatch to fail\n' >&2
  exit 1
fi
[ "$(cat "${bad_bin}/runq")" = "old-client" ]

if RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
  RUNQ_VERSION=vtest \
  RUNQ_OS=darwin \
  RUNQ_ARCH=arm64 \
  RUNQ_INSTALL_DIR="${tmp_dir}/invalid-bin" \
  RUNQ_WITH_UI=maybe \
    sh "${repo_root}/install.sh" >/dev/null 2>&1; then
  printf 'expected invalid RUNQ_WITH_UI to fail\n' >&2
  exit 1
fi

# ── The service: restart a running one, start a stopped one ─────────────────
daemon_release="${tmp_dir}/releases/vdaemon"
mkdir -p "$daemon_release"
cat > "${daemon_release}/runq-linux-amd64" <<'FAKE'
#!/bin/sh
state=${FAKE_RUNQ_STATE:?}
printf '%s\n' "$*" >> "${state}/calls"
case "$*" in
  "serve status") [ -e "${state}/running" ] ;;
  "serve restart"|"serve -d") : > "${state}/running" ;;
  *) printf 'daemon-fake\n' ;;
esac
FAKE
chmod +x "${daemon_release}/runq-linux-amd64"
write_checksum "${daemon_release}/runq-linux-amd64" "${daemon_release}/runq-linux-amd64.sha256"

daemon_install() {
  RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
  RUNQ_VERSION=vdaemon \
  RUNQ_OS=linux \
  RUNQ_ARCH=amd64 \
  RUNQ_INSTALL_DIR="$1" \
  RUNQ_WITH_UI=0 \
  RUNQ_START_DAEMON=1 \
  RUNQ_DATA_DIR="${tmp_dir}/daemon-data" \
  FAKE_RUNQ_STATE="$2" \
    sh "${repo_root}/install.sh" > "${2}/install.log" 2>&1 < /dev/null
}

running_state="${tmp_dir}/running-state"
mkdir -p "$running_state"
: > "${running_state}/running"
daemon_install "${tmp_dir}/running-bin" "$running_state" \
  || { printf 'install with a running service failed:\n' >&2; cat "${running_state}/install.log" >&2; exit 1; }
grep -qx 'serve restart' "${running_state}/calls" \
  || { printf 'a running service was not restarted:\n' >&2; cat "${running_state}/install.log" >&2; exit 1; }
if grep -qx 'serve -d' "${running_state}/calls"; then
  printf 'a running service got a second copy started\n' >&2
  exit 1
fi
grep -q 'runq daemon is ready' "${running_state}/install.log"

stopped_state="${tmp_dir}/stopped-state"
mkdir -p "$stopped_state"
daemon_install "${tmp_dir}/stopped-bin" "$stopped_state"
grep -qx 'serve -d' "${stopped_state}/calls"
grep -q 'runq daemon is ready' "${stopped_state}/install.log"

# ── Start at login ──────────────────────────────────────────────────────────
# Login items are per user: as root the installer skips them, and so do we.
if [ "$(id -u)" -eq 0 ]; then
  printf 'installer smoke tests passed (login items skipped as root)\n'
  exit 0
fi
make_asset runq-linux-arm64 core-linux-arm64
fake_bin="${tmp_dir}/fake-bin"
mkdir -p "$fake_bin"
fake_log="${tmp_dir}/fake-calls"

autostart_install() { # os home wsl-distro autostart
  if [ -n "$3" ]; then
    WSL_DISTRO_NAME=$3
    export WSL_DISTRO_NAME
  else
    unset WSL_DISTRO_NAME
  fi
  HOME="$2" \
  XDG_CONFIG_HOME="${2}/.config" \
  PATH="${fake_bin}:${PATH}" \
  FAKE_LOG="$fake_log" \
  RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
  RUNQ_VERSION=vtest \
  RUNQ_OS="$1" \
  RUNQ_ARCH=arm64 \
  RUNQ_INSTALL_DIR="${2}/bin" \
  RUNQ_WITH_UI=0 \
  RUNQ_AUTOSTART="$4" \
    sh "${repo_root}/install.sh" < /dev/null
  unset WSL_DISTRO_NAME
}

# macOS: a LaunchAgent that runs `runq serve` at login, removed again with 0.
mac_home="${tmp_dir}/mac-home"
autostart_install darwin "$mac_home" "" 1 > /dev/null
plist="${mac_home}/Library/LaunchAgents/io.github.gliese129.runq.plist"
grep -q "<string>${mac_home}/bin/runq</string>" "$plist"
grep -q '<string>serve</string>' "$plist"
grep -q "<string>${mac_home}/.local/share/runq/daemon.log</string>" "$plist"
grep -q '<key>RunAtLoad</key><true/>' "$plist"
if grep -q 'KeepAlive' "$plist"; then
  printf 'the login item must not restart a service that exits\n' >&2
  exit 1
fi
if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$plist" > /dev/null
fi
autostart_install darwin "$mac_home" "" 0 > /dev/null
[ ! -e "$plist" ]

# Linux with systemd: a user unit, enabled for the next login, not started now.
cat > "${fake_bin}/systemctl" <<'FAKE'
#!/bin/sh
printf 'systemctl %s\n' "$*" >> "${FAKE_LOG:?}"
FAKE
chmod +x "${fake_bin}/systemctl"
linux_home="${tmp_dir}/linux-home"
autostart_install linux "$linux_home" "" 1 > /dev/null
unit="${linux_home}/.config/systemd/user/runq.service"
grep -qx "ExecStart=\"${linux_home}/bin/runq\" serve" "$unit"
grep -qx "StandardError=append:${linux_home}/.local/share/runq/daemon.log" "$unit"
grep -qx 'systemctl --user enable runq.service' "$fake_log"
if grep -q -- '--now' "$fake_log"; then
  printf 'the installer must not start a second service through systemd\n' >&2
  exit 1
fi
autostart_install linux "$linux_home" "" 0 > /dev/null
[ ! -e "$unit" ]
grep -qx 'systemctl --user disable runq.service' "$fake_log"
rm -f "${fake_bin}/systemctl" "$fake_log"

# WSL: a Windows Startup shortcut that runs runq in a headless console.
cat > "${fake_bin}/powershell.exe" <<'FAKE'
#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-EncodedCommand" ]; then
    printf '%s' "$2" | base64 -d | iconv -f UTF-16LE -t UTF-8 >> "${FAKE_LOG:?}"
    printf '\n' >> "${FAKE_LOG:?}"
  fi
  shift
done
printf 'C:\\Users\\r\\Startup\\runq (Ubuntu).lnk\r\n'
FAKE
chmod +x "${fake_bin}/powershell.exe"
wsl_home="${tmp_dir}/wsl-home"
autostart_install linux "$wsl_home" Ubuntu 1 > "${tmp_dir}/wsl.log"
grep -q 'conhost.exe' "$fake_log"
grep -q -- '--headless wsl.exe -d "Ubuntu" --exec ' "$fake_log"
grep -q "exec ''${wsl_home}/bin/runq'' serve >>''${wsl_home}/.local/share/runq/daemon.log'' 2>&1" "$fake_log"
grep -q 'runq (Ubuntu).lnk' "$fake_log"
grep -q 'start when you log in to Windows' "${tmp_dir}/wsl.log"
autostart_install linux "$wsl_home" Ubuntu 0 > /dev/null
grep -q 'Remove-Item' "$fake_log"
rm -f "${fake_bin}/powershell.exe" "$fake_log"

# Without a terminal and without RUNQ_AUTOSTART, an existing login item stays.
autostart_install darwin "$mac_home" "" 1 > /dev/null
HOME="$mac_home" \
RUNQ_DOWNLOAD_BASE="file://${tmp_dir}/releases" \
RUNQ_VERSION=vtest \
RUNQ_OS=darwin \
RUNQ_ARCH=arm64 \
RUNQ_INSTALL_DIR="${mac_home}/bin" \
RUNQ_WITH_UI=0 \
  sh "${repo_root}/install.sh" > /dev/null < /dev/null
[ -e "$plist" ]

printf 'installer smoke tests passed\n'
