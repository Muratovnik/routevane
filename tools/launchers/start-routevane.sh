#!/bin/sh
# Start the ready-to-run application. See README.txt beside this file.
set -eu
cd "$(dirname "$0")"
port="${1:-8765}"
if [ ! -f ./routevane ]; then
    echo 'Missing routevane. Extract the whole archive.' >&2
    exit 1
fi
if [ ! -x ./routevane ]; then chmod +x ./routevane; fi
open_browser=true
if [ "${ROUTEVANE_NO_BROWSER:-}" = 1 ]; then open_browser=false; fi
echo "Routevane: http://127.0.0.1:${port}"
echo 'Press Ctrl+C to stop. Your data is in the data folder beside this launcher.'
exec ./routevane serve --port "$port" --open-browser="$open_browser"
