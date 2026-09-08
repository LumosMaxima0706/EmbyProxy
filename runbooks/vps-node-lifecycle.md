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
signed remote-cleanup dispatch, remote acceptance, remote cleanup completion,
owned DNS cleanup, credential revocation, and controller routing/state cleanup.
For an online edge, the controller signs a fixed `DECOMMISSION` job containing
job ID, node ID, nonce, TTL, and a single-use completion token. The edge polls
that control channel, verifies the pinned Controller Ed25519 public key, and
can execute only the fixed self-uninstall cleanup. A short-lived transient
cleanup helper reports completion after the edge service has stopped; it has no
normal node credential and is idempotent.

An unreachable edge is excluded and revoked immediately, and owned DNS is
removed by immutable provider record ID. The job remains `partial` with
`remote_cleanup_pending=true` and exposes the short-lived manual cleanup
command. Retry re-drives the failed DNS/dispatch/completion step only; it never
restores credentials or scheduler eligibility. The production Spaceship mode
uses the existing restricted adapter's `delete-record --record-id ... --type
A|AAAA` operation. A missing record is successful, and non-owned records are
never selected.

The Controller signing key is stored only in the private Controller SQLite KV
store (`edgecontrol:decommission:ed25519`) and is not emitted in bootstrap,
admin responses, or logs. Edges pin the public key in their local config. The
key is intentionally stable across Controller restarts; rotation requires an
overlap window in which old edges are re-enrolled with the new public key before
the old key is retired. Until that coordinated rollout, mixed versions keep
normal heartbeat/playback working, while an edge that does not advertise
`decommission_capable` is shown as remote cleanup unsupported rather than being
promised a complete removal.

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
