package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// PutJSONPair commits related JSON snapshots together or leaves both unchanged.
func (kv *KV) PutJSONPair(ctx context.Context, firstKey, secondKey string, value any) error {
	if kv == nil || kv.store == nil || firstKey == "" || secondKey == "" || firstKey == secondKey {
		return errors.New("invalid_kv_pair")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tx, err := kv.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	statement := `INSERT INTO proxy_kv(k,v,updated_at) VALUES(?,?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v,updated_at=excluded.updated_at`
	if _, err = tx.ExecContext(ctx, statement, firstKey, string(raw), now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, statement, secondKey, string(raw), now); err != nil {
		return err
	}
	return tx.Commit()
}
