#!/bin/bash
set -euo pipefail

test "$(id -u)" = 0
release=${1:?built publication-agent binary path is required}
test -x "$release"
install -d -o root -g root -m 700 /etc/embyproxy-publication-agent
install -d -o root -g root -m 0750 /etc/nginx/conf.d/embyproxy-publications
install -o root -g root -m 755 "$release" /usr/local/sbin/embyproxy-publication-edge
install -o root -g root -m 600 deploy/publication/edge-nosla.example.json /etc/embyproxy-publication-agent/edge-nosla.json
printf '%s\n' 'NOSLA_EDGE_HELPER_INSTALLED=YES' 'NOSLA_AGENT_SERVICE_NOT_STARTED=YES'
