package failover

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

// SpaceshipRecordDeleter invokes the existing restricted adapter. Credentials
// remain in its root-only env file and never enter Controller arguments or
// logs. The adapter is asked to delete one immutable record ID and type.
type SpaceshipRecordDeleter struct {
	AdapterPath string
}

func (p SpaceshipRecordDeleter) ProviderMode() DNSProviderMode { return DNSProviderModeExternal }

func (p SpaceshipRecordDeleter) DeleteRecord(ctx context.Context, recordID string) error {
	return p.DeleteRecordType(ctx, recordID, "A")
}

func (p SpaceshipRecordDeleter) DeleteRecordType(ctx context.Context, recordID, recordType string) error {
	if strings.TrimSpace(p.AdapterPath) == "" || strings.TrimSpace(recordID) == "" || (recordType != "A" && recordType != "AAAA") {
		return errors.New("invalid_spaceship_delete_request")
	}
	cmd := exec.CommandContext(ctx, "python3", p.AdapterPath, "delete-record", "--record-id", recordID, "--type", recordType)
	out, err := cmd.Output()
	if err != nil {
		return errors.New("spaceship_delete_failed")
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(out, &result) != nil || !result.OK {
		return errors.New("spaceship_delete_failed")
	}
	return nil
}

// DeleteOwnedRecord keeps the controller-side ownership tuple attached to the
// provider operation. The adapter still deletes only the immutable record ID;
// provider/account/zone/node metadata can never be substituted by a hostname.
func (p SpaceshipRecordDeleter) DeleteOwnedRecord(ctx context.Context, provider, account, zone, nodeID, recordID, recordType string) error {
	if provider != "spaceship" || strings.TrimSpace(account) == "" || strings.TrimSpace(zone) == "" || strings.TrimSpace(nodeID) == "" {
		return errors.New("dns_ownership_unverified")
	}
	return p.DeleteRecordType(ctx, recordID, recordType)
}
