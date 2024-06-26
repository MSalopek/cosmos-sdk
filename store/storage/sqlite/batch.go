package sqlite

import (
	"database/sql"
	"fmt"

	"cosmossdk.io/store/v2"
)

var _ store.Batch = (*Batch)(nil)

type batchAction int

const (
	batchActionSet batchAction = 0
	batchActionDel batchAction = 1
)

type batchOp struct {
	action     batchAction
	storeKey   []byte
	key, value []byte
}

type Batch struct {
	db      *sql.DB
	tx      *sql.Tx
	ops     []batchOp
	size    int
	version uint64
}

func NewBatch(db *sql.DB, version uint64) (*Batch, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to create SQL transaction: %w", err)
	}

	return &Batch{
		db:      db,
		tx:      tx,
		ops:     make([]batchOp, 0),
		version: version,
	}, nil
}

func (b *Batch) Size() int {
	return b.size
}

func (b *Batch) Reset() error {
	b.ops = nil
	b.ops = make([]batchOp, 0)
	b.size = 0

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}

	b.tx = tx
	return nil
}

func (b *Batch) Set(storeKey []byte, key, value []byte) error {
	b.size += len(key) + len(value)
	b.ops = append(b.ops, batchOp{action: batchActionSet, storeKey: storeKey, key: key, value: value})
	return nil
}

func (b *Batch) Delete(storeKey []byte, key []byte) error {
	b.size += len(key)
	b.ops = append(b.ops, batchOp{action: batchActionDel, storeKey: storeKey, key: key})
	return nil
}

func (b *Batch) Write() error {
	_, err := b.tx.Exec(reservedUpsertStmt, reservedStoreKey, keyLatestHeight, b.version, 0, b.version)
	if err != nil {
		return fmt.Errorf("failed to exec SQL statement: %w", err)
	}

	for _, op := range b.ops {
		switch op.action {
		case batchActionSet:
			_, err := b.tx.Exec(upsertStmt, op.storeKey, op.key, op.value, b.version, op.value)
			if err != nil {
				return fmt.Errorf("failed to exec SQL statement: %w", err)
			}

		case batchActionDel:
			_, err := b.tx.Exec(delStmt, b.version, op.storeKey, op.key, b.version)
			if err != nil {
				return fmt.Errorf("failed to exec SQL statement: %w", err)
			}
		}
	}

	if err := b.tx.Commit(); err != nil {
		return fmt.Errorf("failed to write SQL transaction: %w", err)
	}

	return nil
}
