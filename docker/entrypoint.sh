#!/bin/sh
# On the first start, settings that let browsers outside the container in:
# the home network (plain HTTP and a password, from private addresses only,
# which is how Docker's port forwarding arrives), and the tunnel's HTTPS
# address when QUARTERS_URL is set.
set -e
cfg="$HOME/.config/quarters/config.json"
if [ ! -f "$cfg" ]; then
  mkdir -p "$(dirname "$cfg")"
  if [ -n "$QUARTERS_URL" ]; then
    remote="{\"url\": \"$QUARTERS_URL\", \"lan\": true}"
  else
    remote='{"lan": true}'
  fi
  printf '{\n  "projects": [],\n  "remote": %s\n}\n' "$remote" > "$cfg"
fi
exec "$@"
