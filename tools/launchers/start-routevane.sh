#!/bin/sh
# Run Routevane. Nothing else is needed: the control surface is inside the binary
# next to this file, and the catalog it reads is the directory beside it.
#
# Pass a port as the first argument to use one other than 8765.
set -eu

cd "$(dirname "$0")"
port="${1:-8765}"

if [ ! -x ./routing-agent ]; then
    if [ -f ./routing-agent ]; then
        # A zip archive does not carry the executable bit, so restore it rather
        # than making the operator find that out from a permission error.
        chmod +x ./routing-agent
    else
        echo 'Routevane: routing-agent is not next to this file.' >&2
        echo 'Unpack the whole archive and run the copy inside it.' >&2
        exit 1
    fi
fi

# The page is opened a moment later, in the background, so it is not requested
# before the listener answers and so a browser failure can never stop the
# service.
open_page() {
    sleep 2
    url="http://127.0.0.1:${port}"
    if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$url" >/dev/null 2>&1 || true
    elif command -v open >/dev/null 2>&1; then
        open "$url" >/dev/null 2>&1 || true
    fi
}
open_page &

echo "Routevane: http://127.0.0.1:${port}"
echo 'Press Ctrl+C to stop.'
echo
exec ./routing-agent serve --port "$port"
