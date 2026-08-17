#!/bin/bash
set -euo pipefail

test "$(id -u)" = 0
release=${1:?built publication-agent binary path is required}
test -x "$release"
install -d -o root -g root -m 700 /etc/embyproxy-publication-agent
install -d -o root -g embyproxy-gsy-sidecar -m 2770 /etc/nginx/conf.d/embyproxy-publications
install -o root -g root -m 755 "$release" /usr/local/sbin/embyproxy-publication-agent
install -o root -g root -m 755 "$release" /usr/local/sbin/embyproxy-publication-edge
install -o root -g root -m 644 deploy/publication/embyproxy-publication-agent.service /etc/systemd/system/embyproxy-publication-agent.service
install -o root -g root -m 600 deploy/publication/edge-bwg.example.json /etc/embyproxy-publication-agent/edge-bwg.json
systemctl daemon-reload
printf '%s\n' 'BWG_AGENT_BINARY_INSTALLED=YES' 'BWG_AGENT_SERVICE_NOT_STARTED=YES'
