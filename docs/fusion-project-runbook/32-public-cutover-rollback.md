# Public Cutover Rollback

Status: BACKUP VERIFIED - EXACT CANARY HOSTNAME PENDING

Rollback must be scoped to the files actually changed. The exact timestamped
backup directory and hashes will be recorded before any mutation. The command
sequence is:

```bash
# The canary is a new file, so rollback removes only that new file.
rm -f /etc/nginx/conf.d/embyproxy-gsy-canary.conf
nginx -t && systemctl reload nginx
# Existing stream/staging/rathole files are not modified by this canary.
# Remove or restore only the new DNS record through the secure provider tool.
# then re-run bounded public and localhost healthchecks
```

Verified backup root:
`/root/backups/embyproxy-public-cutover/20260812T014601Z`.

If an unexpected mutation affects an existing file, restore only that exact
file from its matching subdirectory in the verified backup, run `nginx -t`, and
reload Nginx. Do not bulk-copy unrelated configuration files.

The sidecar release/config/database rollback remains the existing target-scoped
procedure in `20-rollback-plan.md`; it must not delete the deployment directory
or clear the database. If DNS apply fails, preserve the previous record and do
not commit an internal active-state transition.
