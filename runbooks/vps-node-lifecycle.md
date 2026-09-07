# VPS Node Lifecycle

The controller treats scheduling, draining, revocation, and removal as separate
operations.

## States

- `healthy`: admitted to the scheduler when all health gates pass.
- `disabled`: the edge remains enrolled and connected, but receives no new
  sessions. It can be enabled again.
- `draining`: no new sessions are admitted; existing sessions finish normally.
- `revoked`: node credentials and enrollments are invalid. Use **重新接入/重装**;
  never enable it directly.
- `decommissioning`: a removal job is running.
- `removed`: archived control-plane record. It is hidden by the default UI
  filter.

## Operations

Use the node row's primary action for switching or enabling. Put drain,
disable/enable scheduling, re-enrollment, and removal under the `...` menu.
Removal requires typing the exact node name. If the active node has no healthy
fallback, ordinary removal is rejected; force removal explicitly warns that
playback may be interrupted.

The removal job records these steps: traffic switch, drain, scheduler exclude,
credential revocation, remote cleanup, DNS cleanup, and controller routing/state
cleanup. A stale heartbeat produces `partial` completion and a short-lived SSH
cleanup command. Control-plane revocation and routing cleanup still complete;
the job can be retried after the VPS becomes reachable.

## Ownership

Only resources marked in `proxy_node_ownership` may be removed:
`dns_record_id/dns_owned`, `caddy_installed_by_project`,
`caddy_config_owned`, `tls_state_owned`, and `edge_unit_owned`. Non-owned Caddy
configuration, certificates, and unrelated server data are never touched.

## Diagnostics

1. `GET /api/admin/proxy-nodes` for state and health gates.
2. `GET /api/admin/proxy-node-jobs/<job-id>` for step progress.
3. `POST /api/admin/proxy-node-jobs/<job-id>/retry` after remote cleanup.
4. Verify scheduler eligibility, heartbeat freshness, ingress/playback health,
   and the edge service journal before re-enabling a node.
