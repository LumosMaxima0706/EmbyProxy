# Embykeeper Weak Integration

## Scope

EmbyProxy exposes a protected, read-only link and status summary for an
independently deployed Embykeeper service. It does not vendor Embykeeper GPL
source, run its process, share its database, or manage credentials.

Full deployment materials live in the separate `embykeeper-deploy` repository:

- Docker Compose and systemd examples
- install, healthcheck, validation, and rollback scripts
- status exporter examples
- deployment, security, operations, and troubleshooting documents

## EmbyProxy configuration

```dotenv
EMBYKEEPER_INTEGRATION_ENABLED=false
EMBYKEEPER_EXTERNAL_URL=
EMBYKEEPER_STATUS_FILE=
EMBYKEEPER_DISPLAY_NAME=Embykeeper
```

The integration is disabled by default. When enabled, the external URL must be
HTTPS without credentials, query, or fragment. The status path must be absolute,
end in `status.json`, refer to a regular non-symlink file, and be no larger than
64 KiB.

## Admin behavior

Authenticated Admin users see a "Keeper tasks / Embykeeper" tab. It provides:

- disabled/unavailable/available state;
- `last_success`, `next_run`, `last_error`, and profile counters;
- a new-tab HTTPS link with `noopener noreferrer`;
- a placeholder-only configuration template download.

There are no start, stop, login, apply, sync, or secret-management actions.
The UI never embeds or proxies the external console.

## Status contract

The independent service or a local wrapper may publish a sanitized JSON file:

```json
{
  "last_success": "2026-08-18T12:00:00Z",
  "next_run": "2026-08-25T12:00:00Z",
  "last_error": "",
  "enabled_profiles_count": 1,
  "failed_profiles_count": 0
}
```

Times are RFC3339 or empty. Counts are non-negative and failed cannot exceed
enabled. `last_error` is an uppercase code only. Unknown fields, raw errors,
credentials, URLs, account names, sessions, and cache data are forbidden.
Write the file atomically. Missing or malformed status is shown as unavailable,
never as a server error.

## Enrollment boundary

Saving or publishing an Emby server in EmbyProxy never enrolls it in
Embykeeper. The operator must manually create a profile in the standalone
server-side secrets directory, keep it disabled until site rules/TOS are
reviewed, and enable only an operator-owned low-risk account.

## Security and rollback

- Do not place `config.toml`, `cache.json`, `*.session`, `.env`, databases,
  logs, certificates, or real URLs in Git.
- Do not expose Embykeeper WebUI directly or reuse EmbyProxy Admin credentials.
- Do not let Embykeeper access EmbyProxy SQLite, Nginx, edge helper,
  publication-agent, DNS, or failover files.
- Disable `EMBYKEEPER_INTEGRATION_ENABLED` to hide the link/status summary.
- Runtime rollback stops/restores only the standalone Embykeeper deployment;
  it never changes EmbyProxy playback or routing state.

See `embykeeper-deploy/docs/DEPLOYMENT.md` and `embykeeper-deploy/docs/ROLLBACK.md`
for the separate repository's procedures.
