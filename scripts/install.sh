#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-only
# Keep the invocation last: a truncated pipe must not start installation.
set -eu
fail() { printf 'ShellStudio installer: %s\n' "$*" >&2; exit 1; }
fetch() {
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
        --connect-timeout 10 --max-time 120 --retry 2 "$@"
}
tmux_ready() {
    command -v tmux >/dev/null 2>&1 &&
        tmux -V | awk '{split($2,v,"."); exit !(v[1]>3 || (v[1]==3 && v[2]+0>=4))}'
}
install_tmux() {
    tmux_ready && return
    [ "$skip_deps" = 1 ] && return
    printf 'ShellStudio needs tmux 3.4 or newer.\n'
    if [ "$assume_yes" != 1 ]; then
        [ -r /dev/tty ] || fail "install tmux first, or rerun with --yes to install dependencies"
        printf 'Install tmux using your system package manager? [y/N] ' >/dev/tty
        read -r answer </dev/tty || fail "no answer received"
        case "$answer" in y|Y|yes|YES) ;; *) fail "tmux installation declined" ;; esac
    fi
    privilege=
    if [ "$(id -u)" != 0 ]; then
        command -v sudo >/dev/null 2>&1 || fail "sudo is needed to install tmux"
        privilege=sudo
    fi
    if command -v apt-get >/dev/null 2>&1; then
        $privilege apt-get update
        $privilege apt-get install -y tmux
    elif command -v dnf >/dev/null 2>&1; then
        $privilege dnf install -y tmux
    elif command -v pacman >/dev/null 2>&1; then
        $privilege pacman -S --needed --noconfirm tmux
    elif command -v zypper >/dev/null 2>&1; then
        $privilege zypper --non-interactive install tmux
    elif command -v apk >/dev/null 2>&1; then
        $privilege apk add tmux
    else
        fail "unsupported package manager; install tmux 3.4+ and rerun"
    fi
    tmux_ready || fail "your distribution did not provide tmux 3.4+; upgrade tmux and rerun"
}
add_path() {
    [ "$bin_dir" = "$HOME/.local/bin" ] || return 0
    for rc in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.zprofile"; do
        case "$rc" in */.bash_profile|*/.bash_login|*/.zprofile) [ -e "$rc" ] || continue ;; esac
        [ ! -L "$rc" ] || { printf 'PATH setup skipped for symlink: %s\n' "$rc"; continue; }
        if ! grep -Fq '# ShellStudio PATH' "$rc" 2>/dev/null; then
            cat >>"$rc" <<'PATH_BLOCK'

# ShellStudio PATH
case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
PATH_BLOCK
        fi
    done
    fish_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d"
    mkdir -p "$fish_dir"
    if [ ! -e "$fish_dir/shellstudio.fish" ] && [ ! -L "$fish_dir/shellstudio.fish" ]; then
        printf '%s\n' '# ShellStudio PATH' 'fish_add_path --path "$HOME/.local/bin"' >"$fish_dir/shellstudio.fish"
    fi
}
main() {
    repo='https://github.com/TobiasCoding/ShellStudio'
    bin_dir="${SHELLSTUDIO_BIN_DIR:-$HOME/.local/bin}"
    from_dir=
    release=
    skip_path=0
    skip_deps=0
    assume_yes=0
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --from) [ "$#" -ge 2 ] || fail "--from requires a release directory"; from_dir=$2; shift 2 ;;
            --version) [ "$#" -ge 2 ] || fail "--version requires vMAJOR.MINOR.PATCH"; release=$2; shift 2 ;;
            --bin-dir) [ "$#" -ge 2 ] || fail "--bin-dir requires an absolute directory"; bin_dir=$2; shift 2 ;;
            --no-path) skip_path=1; shift ;;
            --no-deps) skip_deps=1; shift ;;
            --yes) assume_yes=1; shift ;;
            --help|-h)
                printf '%s\n' 'Usage: sh install.sh [--version vX.Y.Z] [--bin-dir DIR] [--from RELEASE_DIR]' \
                    '  --yes      Permit installing missing tmux with the system package manager' \
                    '  --no-deps  Skip the tmux prerequisite check' \
                    '  --no-path  Leave shell startup files unchanged'
                return ;;
            *) fail "unknown option: $1" ;;
        esac
    done
    [ "$(uname -s)" = Linux ] || fail "Linux or WSL2 is required"
    case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail "supported architectures: amd64, arm64" ;; esac
    case "$bin_dir" in /*) ;; *) fail "--bin-dir must be absolute" ;; esac
    for tool in sha256sum awk mktemp flock timeout; do command -v "$tool" >/dev/null 2>&1 || fail "missing dependency: $tool"; done
    if [ -z "$from_dir" ]; then command -v curl >/dev/null 2>&1 || fail "curl is required"; fi
    umask 077
    mkdir -p "$bin_dir"
    [ ! -L "$bin_dir/.shellstudio-install.lock" ] || fail "installation lock must not be a symlink"
    exec 9>"$bin_dir/.shellstudio-install.lock"
    flock -n 9 || fail "another installer or updater is running"
    stage=$(mktemp -d "$bin_dir/.shellstudio-install.XXXXXXXX")
    trap 'rm -rf "$stage"' EXIT
    trap 'exit 1' HUP INT TERM
    asset="shellstudio-linux-$arch"
    if [ -n "$from_dir" ]; then
        cp "$from_dir/$asset" "$stage/shellstudio"
        cp "$from_dir/SHA256SUMS" "$stage/SHA256SUMS"
    else
        if [ -z "$release" ]; then
            latest=$(fetch --output /dev/null --write-out '%{url_effective}' "$repo/releases/latest") ||
                fail "no public GitHub release is available; check repository visibility and published releases"
            case "$latest" in "$repo/releases/tag/"*) release=${latest##*/} ;; *) fail "could not resolve a stable GitHub release" ;; esac
        fi
        printf '%s\n' "$release" | awk '/^v?[0-9]+\.[0-9]+\.[0-9]+$/ {ok=1} END {exit !ok}' ||
            fail "release must be a stable vMAJOR.MINOR.PATCH tag"
        fetch --max-filesize 1048576 "$repo/releases/download/$release/SHA256SUMS" --output "$stage/SHA256SUMS"
        fetch --max-filesize 134217728 "$repo/releases/download/$release/$asset" --output "$stage/shellstudio"
    fi
    expected=$(awk -v name="$asset" '$2==name || $2=="*"name {print $1}' "$stage/SHA256SUMS")
    [ "${#expected}" -eq 64 ] || fail "missing or duplicate SHA-256 checksum"
    case "$expected" in *[!0-9a-f]*) fail "invalid SHA-256 checksum" ;; esac
    actual=$(sha256sum "$stage/shellstudio"); actual=${actual%% *}
    [ "$actual" = "$expected" ] || fail "checksum mismatch; current installation has not been changed"
    chmod 755 "$stage/shellstudio"
    actual_version=$(timeout 5 "$stage/shellstudio" --version) || fail "downloaded executable cannot run"
    printf '%s\n' "$actual_version" | awk '/^ShellStudio [0-9]+\.[0-9]+\.[0-9]+$/ {ok=1} END {exit !ok}' ||
        fail "unexpected executable version"
    if [ -n "$release" ]; then
        [ "$actual_version" = "ShellStudio ${release#v}" ] || fail "executable version does not match the requested release"
    fi
    install_tmux
    [ ! -L "$bin_dir/shellstudio" ] || fail "refusing to replace a symlink; choose another --bin-dir"
    [ ! -e "$bin_dir/shellstudio" ] || [ -f "$bin_dir/shellstudio" ] || fail "destination is not a regular file"
    mv -f "$stage/shellstudio" "$bin_dir/shellstudio"
    if [ "$skip_path" = 0 ]; then add_path; fi
    printf '\nInstalled %s at %s/shellstudio\n' "$actual_version" "$bin_dir"
    case ":$PATH:" in
        *":$bin_dir:"*) printf 'Run shellstudio from any folder, or shellstudio .\n' ;;
        *) printf 'Open a new terminal, or add %s to PATH in this shell.\n' "$bin_dir" ;;
    esac
    printf 'New releases are checked at interactive startup and installed only after confirmation.\n'
}
main "$@"
