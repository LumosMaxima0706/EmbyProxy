# Public Cutover Rollback

Status: READY IN PRINCIPLE - BACKUP PATHS TO BE CREATED AFTER OWNER SCOPE

Rollback must be scoped to the files actually changed. The exact timestamped
backup directory and hashes will be recorded before any mutation. The command
sequence is:

```bash
# on BWG, replace BACKUP with the verified timestamped path
install -m 0644 BACKUP/nginx/*.conf /etc/nginx/conf.d/
nginx -t && systemctl reload nginx
# only if rathole was explicitly changed
install -m 0600 BACKUP/rathole/server.toml /etc/rathole/server.toml
systemctl restart rathole.service
systemctl is-active rathole.service
# restore the prior DNS record through the owner-approved provider/API
# then re-run bounded public and localhost healthchecks
```

The sidecar release/config/database rollback remains the existing target-scoped
procedure in `20-rollback-plan.md`; it must not delete the deployment directory
or clear the database. If DNS apply fails, preserve the previous record and do
not commit an internal active-state transition.
