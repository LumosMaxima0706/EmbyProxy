# EmbyProxy weak integration example

This directory is intentionally minimal. EmbyProxy does not run, install,
update, or control Embykeeper. Use the separate `embykeeper-deploy` repository
for Docker Compose, systemd, install, healthcheck, status-exporter, and rollback
procedures.

The files kept here are safe placeholders only:

- `config.example.toml` contains a disabled profile and replacement markers.
- `status.example.json` shows the five-field read-only status contract.
- `.gitignore` blocks runtime credentials, cache, sessions, databases, logs, and
  local Compose files.

The Admin integration accepts only an HTTPS external URL and an absolute,
sanitized `status.json` path. It never reads `config.toml`, `cache.json`,
Telegram sessions, raw logs, or EmbyProxy database contents.
