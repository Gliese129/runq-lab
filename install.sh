#!/bin/sh

set -eu

REPOSITORY=${RUNQ_REPOSITORY:-Gliese129/runq-lab}
DOWNLOAD_BASE=${RUNQ_DOWNLOAD_BASE:-https://github.com/${REPOSITORY}/releases/download}
API_BASE=${RUNQ_API_BASE:-https://api.github.com/repos/${REPOSITORY}}

say() {
  printf '%s\n' "$*"
}

warn() {
  printf 'runq installer: warning: %s\n' "$*" >&2
}

die() {
  printf 'runq installer: error: %s\n' "$*" >&2
  exit 1
}

bool_value() {
  case "$1" in
    1|true|TRUE|yes|YES|y|Y|on|ON) printf '1\n' ;;
    0|false|FALSE|no|NO|n|N|off|OFF) printf '0\n' ;;
    *) return 1 ;;
  esac
}

command -v curl >/dev/null 2>&1 || die "curl is required"

raw_os=${RUNQ_OS:-$(uname -s)}
case "$raw_os" in
  Linux|linux) os=linux ;;
  Darwin|darwin|macOS|macos) os=darwin ;;
  *) die "unsupported operating system: ${raw_os} (Windows support is planned later)" ;;
esac

raw_arch=${RUNQ_ARCH:-$(uname -m)}
case "$raw_arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: ${raw_arch}" ;;
esac

version=${RUNQ_VERSION:-latest}
if [ "$version" = "latest" ]; then
  release_json=$(curl -fsSL --retry 3 "${API_BASE}/releases/latest") \
    || die "could not resolve the latest release; set RUNQ_VERSION explicitly"
  version=$(printf '%s\n' "$release_json" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | sed -n '1p')
  [ -n "$version" ] \
    || die "latest release response did not include a tag; set RUNQ_VERSION explicitly"
else
  case "$version" in
    v*) ;;
    *) version="v${version}" ;;
  esac
fi
case "$version" in
  ''|*[!A-Za-z0-9._+-]*) die "invalid release version: ${version}" ;;
esac

if [ "${RUNQ_WITH_UI+x}" = "x" ]; then
  with_ui=$(bool_value "$RUNQ_WITH_UI") \
    || die "RUNQ_WITH_UI must be yes/no, true/false, or 1/0"
elif (exec </dev/tty) 2>/dev/null; then
  printf 'Install runq with the embedded web UI? [y/N] ' > /dev/tty
  answer=
  IFS= read -r answer < /dev/tty || true
  case "$answer" in
    y|Y|yes|YES) with_ui=1 ;;
    *) with_ui=0 ;;
  esac
else
  with_ui=0
  warn "no interactive terminal; installing the core CLI (set RUNQ_WITH_UI=1 to select the UI build)"
fi

if [ "${RUNQ_START_DAEMON+x}" = "x" ]; then
  start_daemon=$(bool_value "$RUNQ_START_DAEMON") \
    || die "RUNQ_START_DAEMON must be yes/no, true/false, or 1/0"
else
  start_daemon=0
fi

# Start the service at login. Unset at a terminal: ask (default yes). Unset
# without one: leave any existing entry as it is. 0 removes the entry.
if [ "${RUNQ_AUTOSTART+x}" = "x" ]; then
  autostart=$(bool_value "$RUNQ_AUTOSTART") \
    || die "RUNQ_AUTOSTART must be yes/no, true/false, or 1/0"
elif (exec </dev/tty) 2>/dev/null; then
  printf 'Start the runq service automatically when you log in? [Y/n] ' > /dev/tty
  answer=
  IFS= read -r answer < /dev/tty || true
  case "$answer" in
    n|N|no|NO) autostart=0 ;;
    *) autostart=1 ;;
  esac
else
  autostart=
fi

if [ "$with_ui" = "1" ]; then
  runq_asset="runq-dashboard-${os}-${arch}"
else
  runq_asset="runq-${os}-${arch}"
fi

if [ "${RUNQ_INSTALL_DIR+x}" = "x" ]; then
  install_dir=$RUNQ_INSTALL_DIR
elif [ "$os" = "darwin" ] && [ -d /opt/homebrew/bin ] && [ -w /opt/homebrew/bin ]; then
  install_dir=/opt/homebrew/bin
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
  install_dir=/usr/local/bin
else
  install_dir=${HOME}/.local/bin
fi

[ -n "$install_dir" ] || die "install directory cannot be empty"
mkdir -p "$install_dir" || die "could not create ${install_dir}"
case "$install_dir" in
  /*) ;;
  *) install_dir=$(CDPATH= cd -- "$install_dir" && pwd) || die "could not resolve ${install_dir}" ;;
esac
[ -w "$install_dir" ] || die "${install_dir} is not writable; set RUNQ_INSTALL_DIR to a writable directory"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/runq-install.XXXXXX") \
  || die "could not create a temporary directory"
staged_path=
cleanup() {
  if [ -n "$staged_path" ]; then
    rm -f "$staged_path"
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT HUP INT TERM

download_asset() {
  asset=$1
  url="${DOWNLOAD_BASE}/${version}/${asset}"
  say "Downloading ${asset} (${version})..."
  curl -fL --retry 3 --progress-bar -o "${tmp_dir}/${asset}" "$url" \
    || die "could not download ${url}"
  curl -fsSL --retry 3 -o "${tmp_dir}/${asset}.sha256" "${url}.sha256" \
    || die "could not download checksum for ${asset}"
}

verify_asset() {
  asset=$1
  expected=$(sed -n '1{s/[[:space:]].*$//;p;}' "${tmp_dir}/${asset}.sha256")
  case "$expected" in
    *[!0-9A-Fa-f]*|'') die "invalid checksum file for ${asset}" ;;
  esac
  [ "${#expected}" -eq 64 ] || die "invalid checksum file for ${asset}"

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "${tmp_dir}/${asset}" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{print $1}')
  else
    die "sha256sum or shasum is required to verify downloads"
  fi
  [ "$actual" = "$expected" ] || die "checksum verification failed for ${asset}"
}

install_binary_atomic() {
  source=$1
  target=$2
  staged_path=$(mktemp "${install_dir}/.runq.new.XXXXXX") \
    || die "could not create a staging file in ${install_dir}"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$source" "$staged_path" \
      || die "could not stage runq in ${install_dir}"
  else
    cp "$source" "$staged_path" \
      || die "could not stage runq in ${install_dir}"
    chmod 0755 "$staged_path" \
      || die "could not make the staged runq executable"
  fi
  mv -f "$staged_path" "$target" \
    || die "could not replace ${target}"
  staged_path=
}

download_asset "$runq_asset"
verify_asset "$runq_asset"

install_binary_atomic "${tmp_dir}/${runq_asset}" "${install_dir}/runq" \
  || die "could not install runq to ${install_dir}"

say "Installed runq ${version} to ${install_dir}/runq"

case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *)
    warn "${install_dir} is not on PATH"
    warn "add this line to your shell profile: export PATH=\"${install_dir}:\$PATH\""
    ;;
esac

if [ "${RUNQ_DATA_DIR+x}" = "x" ]; then
  data_dir=$RUNQ_DATA_DIR
elif [ "$(id -u)" -eq 0 ]; then
  data_dir=/var/lib/runq
else
  data_dir=${HOME}/.local/share/runq
fi

if [ "$start_daemon" = "1" ]; then
  mkdir -p "$data_dir" \
    || die "runq was installed, but the data directory ${data_dir} could not be created"
  if "${install_dir}/runq" serve status >/dev/null 2>&1; then
    say "Restarting the runq service to activate the installed build..."
    "${install_dir}/runq" serve restart \
      || die "runq was installed, but the existing client daemon could not be restarted"
  else
    say "Starting the runq service in the background..."
    "${install_dir}/runq" serve -d \
      || die "runq was installed, but the daemon could not be started"
  fi

  ready=0
  attempts=0
  while [ "$attempts" -lt 15 ]; do
    if "${install_dir}/runq" serve status >/dev/null 2>&1; then
      ready=1
      break
    fi
    attempts=$((attempts + 1))
    sleep 1
  done
  if [ "$ready" = "1" ]; then
    say "runq daemon is ready"
  else
    warn "the daemon was started but did not become ready within 15 seconds"
    warn "check ${data_dir}/daemon.log and run: runq doctor"
  fi
fi

# ── Start at login ──────────────────────────────────────────────────────────
# Each platform runs `runq serve` in the foreground at login, with the log in
# the same file `runq serve -d` uses. Nothing starts now (the service may be
# running already), and nothing restarts it after a crash: a second copy would
# only fail against the one already running, over and over.

runq_path="${install_dir}/runq"
log_path="${data_dir}/daemon.log"
launch_label=io.github.gliese129.runq

xml_escape() {
  printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

# systemd unit values: % is a specifier, and " and \ end or escape a quote.
unit_escape() {
  printf '%s' "$1" | sed -e 's/%/%%/g' -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

autostart_launchd() {
  agents_dir="${HOME}/Library/LaunchAgents"
  plist="${agents_dir}/${launch_label}.plist"
  if [ "$1" = "0" ]; then
    if [ -e "$plist" ]; then
      rm -f "$plist" || die "could not remove ${plist}"
      say "Removed the login item ${plist}"
    fi
    return 0
  fi
  mkdir -p "$agents_dir" "$data_dir" || die "could not create ${agents_dir}"
  env_entries="    <key>PATH</key><string>$(xml_escape "$PATH")</string>"
  if [ "${RUNQ_DATA_DIR+x}" = "x" ]; then
    env_entries="${env_entries}
    <key>RUNQ_DATA_DIR</key><string>$(xml_escape "$RUNQ_DATA_DIR")</string>"
  fi
  cat > "${plist}.tmp" <<PLIST || die "could not write ${plist}"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>${launch_label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(xml_escape "$runq_path")</string>
    <string>serve</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
${env_entries}
  </dict>
  <key>WorkingDirectory</key><string>/</string>
  <key>StandardOutPath</key><string>$(xml_escape "$log_path")</string>
  <key>StandardErrorPath</key><string>$(xml_escape "$log_path")</string>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
PLIST
  mv -f "${plist}.tmp" "$plist" || die "could not write ${plist}"
  say "runq will start when you log in (${plist})"
}

autostart_systemd() {
  unit_dir="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
  unit="${unit_dir}/runq.service"
  if [ "$1" = "0" ]; then
    if [ -e "$unit" ]; then
      systemctl --user disable runq.service >/dev/null 2>&1 || true
      rm -f "$unit" || die "could not remove ${unit}"
      systemctl --user daemon-reload >/dev/null 2>&1 || true
      say "Removed the login service ${unit}"
    fi
    return 0
  fi
  mkdir -p "$unit_dir" "$data_dir" || die "could not create ${unit_dir}"
  env_line="Environment=\"PATH=$(unit_escape "$PATH")\""
  if [ "${RUNQ_DATA_DIR+x}" = "x" ]; then
    env_line="${env_line} \"RUNQ_DATA_DIR=$(unit_escape "$RUNQ_DATA_DIR")\""
  fi
  cat > "${unit}.tmp" <<UNIT || die "could not write ${unit}"
[Unit]
Description=runq service

[Service]
Type=simple
ExecStart="$(unit_escape "$runq_path")" serve
WorkingDirectory=/
${env_line}
StandardOutput=append:$(unit_escape "$log_path")
StandardError=append:$(unit_escape "$log_path")

[Install]
WantedBy=default.target
UNIT
  mv -f "${unit}.tmp" "$unit" || die "could not write ${unit}"
  systemctl --user daemon-reload \
    || die "wrote ${unit}, but systemctl --user daemon-reload failed"
  systemctl --user enable runq.service >/dev/null 2>&1 \
    || die "wrote ${unit}, but systemctl --user enable runq.service failed"
  say "runq will start when you log in (systemd user service runq.service)"
  say "To start it at boot without logging in: loginctl enable-linger ${USER:-$(id -un)}"
}

# WSL: the distro is not running until something on Windows starts it, and it
# stops once no Windows-side client is attached. A shortcut in the Windows
# Startup folder runs `wsl.exe … runq serve` in a headless console at logon;
# that client staying attached is what keeps the distro, and runq, running.
autostart_wsl() {
  command -v powershell.exe >/dev/null 2>&1 \
    || { warn "powershell.exe is not reachable from WSL (interop off?); skipping the Windows login item"; return 0; }
  command -v iconv >/dev/null 2>&1 && command -v base64 >/dev/null 2>&1 \
    || { warn "iconv and base64 are needed to set the Windows login item; skipping it"; return 0; }
  distro=$WSL_DISTRO_NAME
  shortcut_name="runq (${distro}).lnk"
  if [ "$1" = "0" ]; then
    ps_script="\$l = Join-Path ([Environment]::GetFolderPath('Startup')) '$(printf '%s' "$shortcut_name" | sed "s/'/''/g")'; if (Test-Path -LiteralPath \$l) { Remove-Item -LiteralPath \$l; Write-Output \$l }"
  else
    login_shell=${SHELL:-/bin/sh}
    for value in "$runq_path" "$log_path" "$login_shell" "$distro" "${RUNQ_DATA_DIR:-}"; do
      case "$value" in
        *\"*|*\'*) warn "a path or the distro name contains a quote; skipping the Windows login item"; return 0 ;;
      esac
    done
    mkdir -p "$data_dir" || die "could not create ${data_dir}"
    inner="cd / && exec '${runq_path}' serve >>'${log_path}' 2>&1"
    if [ "${RUNQ_DATA_DIR+x}" = "x" ]; then
      inner="export RUNQ_DATA_DIR='${RUNQ_DATA_DIR}'; ${inner}"
    fi
    # A login shell, so the service sees the same environment as a terminal.
    arguments="--headless wsl.exe -d \"${distro}\" --exec ${login_shell} -lc \"${inner}\""
    ps_arguments=$(printf '%s' "$arguments" | sed "s/'/''/g")
    ps_name=$(printf '%s' "$shortcut_name" | sed "s/'/''/g")
    ps_script="\$l = Join-Path ([Environment]::GetFolderPath('Startup')) '${ps_name}'; \$s = (New-Object -ComObject WScript.Shell).CreateShortcut(\$l); \$s.TargetPath = Join-Path \$env:WINDIR 'System32\\conhost.exe'; \$s.Arguments = '${ps_arguments}'; \$s.WindowStyle = 7; \$s.Description = 'Start the runq service in WSL'; \$s.Save(); Write-Output \$l"
  fi
  encoded=$(printf '%s' "$ps_script" | iconv -f UTF-8 -t UTF-16LE | base64 | tr -d '\n') \
    || die "could not encode the Windows login item command"
  result=$(powershell.exe -NoProfile -NonInteractive -EncodedCommand "$encoded" 2>&1 | tr -d '\r') \
    || die "could not update the Windows login item: ${result}"
  if [ "$1" = "0" ]; then
    [ -z "$result" ] || say "Removed the Windows login item ${result}"
  else
    say "runq will start when you log in to Windows (${result})"
  fi
}

if [ -n "$autostart" ]; then
  if [ "$(id -u)" -eq 0 ]; then
    warn "starting at login is set up per user; skipped for root"
  elif [ "$os" = "darwin" ]; then
    autostart_launchd "$autostart"
  elif [ -n "${WSL_DISTRO_NAME:-}" ]; then
    autostart_wsl "$autostart"
  elif command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    autostart_systemd "$autostart"
  elif [ "$autostart" = "1" ]; then
    warn "no supported login service found (launchd, systemd --user or WSL); start runq with: runq serve -d"
  fi
fi

if [ "$with_ui" = "1" ]; then
  say "Dashboard: http://127.0.0.1:8077"
fi
say "Next: runq doctor"
say "Agents (Codex / Claude Code): runq skill install   # re-run after every upgrade"
