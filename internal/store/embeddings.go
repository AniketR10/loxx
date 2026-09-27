package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// PendingCommand is a command that has no embedding from the current model.
type PendingCommand struct {
	ID   int64
	Text string
}

// Embedding is a command's vector.
type Embedding struct {
	CommandID int64
	Vector    []float32
}

// pendingWhere selects commands without an embedding from model ?.
const pendingWhere = `
	FROM commands c
	LEFT JOIN embeddings e ON e.command_id = c.id AND e.model_id = ?
	WHERE e.command_id IS NULL`

// CountPendingEmbeddings returns how many commands lack an embedding from
// modelID.
func (db *DB) CountPendingEmbeddings(ctx context.Context, modelID string) (int, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*)`+pendingWhere, modelID).Scan(&n)
	return n, err
}

// PendingEmbeddings returns up to limit commands lacking an embedding from
// modelID, most recently used first so the useful ones are ready soonest.
func (db *DB) PendingEmbeddings(ctx context.Context, modelID string, limit int) ([]PendingCommand, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT c.id, c.text`+pendingWhere+` ORDER BY c.last_seen DESC, c.id DESC LIMIT ?`, modelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []PendingCommand
	for rows.Next() {
		var p PendingCommand
		if err := rows.Scan(&p.ID, &p.Text); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// SaveEmbeddings stores vectors from modelID in one transaction, replacing any
// older vector for the same command. Commands deleted in the meantime (e.g.
// by `loc forget`) are skipped rather than failing the batch.
func (db *DB) SaveEmbeddings(ctx context.Context, modelID string, es []Embedding) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO embeddings (command_id, model_id, dims, vector)
		SELECT ?1, ?2, ?3, ?4 WHERE EXISTS (SELECT 1 FROM commands WHERE id = ?1)
		ON CONFLICT (command_id) DO UPDATE SET
			model_id = excluded.model_id, dims = excluded.dims, vector = excluded.vector`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range es {
		if _, err := stmt.ExecContext(ctx, e.CommandID, modelID, len(e.Vector), encodeVector(e.Vector)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Embeddings returns every stored vector from modelID.
func (db *DB) Embeddings(ctx context.Context, modelID string) ([]Embedding, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT command_id, dims, vector FROM embeddings WHERE model_id = ? ORDER BY command_id`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []Embedding
	for rows.Next() {
		var (
			e    Embedding
			dims int
			blob []byte
		)
		if err := rows.Scan(&e.CommandID, &dims, &blob); err != nil {
			return nil, err
		}
		if e.Vector, err = decodeVector(blob, dims); err != nil {
			return nil, fmt.Errorf("embedding for command %d: %w", e.CommandID, err)
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

func encodeVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeVector(b []byte, dims int) ([]float32, error) {
	if len(b) != 4*dims {
		return nil, fmt.Errorf("%d bytes for %d dims", len(b), dims)
	}
	v := make([]float32, dims)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}
