#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <KBRD Agent.app source> <KBRD API URL>" >&2
    exit 1
fi

source_app=$1
api_url=$2
app_dir="$HOME/Applications/KBRD Agent.app"
executable="$app_dir/Contents/MacOS/kbrd-agent"
launch_agents="$HOME/Library/LaunchAgents"
plist="$launch_agents/com.ubikyo.kbrd-agent.plist"
log_dir="$HOME/Library/Logs/KBRD"
log_path="$log_dir/agent.log"
template="$(dirname "$0")/com.ubikyo.kbrd-agent.plist"
domain="gui/$(id -u)"

mkdir -p "$HOME/Applications" "$launch_agents" "$log_dir"
/usr/bin/ditto "$source_app" "$app_dir"

escape_sed() {
    printf '%s' "$1" | sed 's/[&|\\]/\\&/g'
}

sed \
    -e "s|__EXECUTABLE__|$(escape_sed "$executable")|g" \
    -e "s|__API_URL__|$(escape_sed "$api_url")|g" \
    -e "s|__LOG_PATH__|$(escape_sed "$log_path")|g" \
    "$template" > "$plist"
/usr/bin/plutil -lint "$plist" >/dev/null

/bin/launchctl bootout "$domain" "$plist" >/dev/null 2>&1 || true
/bin/launchctl bootstrap "$domain" "$plist"
/bin/launchctl kickstart -k "$domain/com.ubikyo.kbrd-agent"

echo "KBRD Agent installed and started from $app_dir"
