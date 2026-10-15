package app

import (
	"context"
	"database/sql"
)

type idempotencyKey struct{}
type idempotencyRequest struct{ actor, operation, key, requestHash string }

// WithIdempotency ensures the result reference commits with the business write.
// Keys remain valid for at least 30 days (v1 retains this compact metadata).
func WithIdempotency(ctx context.Context, actor, operation, key, requestHash string) context.Context {
	return context.WithValue(ctx, idempotencyKey{}, idempotencyRequest{actor, operation, key, requestHash})
}
func (s *Service) idempotentResult(ctx context.Context, tx *sql.Tx) (string, error) {
	v, ok := ctx.Value(idempotencyKey{}).(idempotencyRequest)
	if !ok {
		return "", nil
	}
	if v.actor == "" || v.operation == "" || v.key == "" || len(v.key) > 200 || v.requestHash == "" {
		return "", ErrInvalid
	}
	var requestHash, result string
	e := tx.QueryRowContext(ctx, `SELECT request_hash,result FROM idempotency WHERE actor=? AND operation=? AND key=?`, v.actor, v.operation, v.key).Scan(&requestHash, &result)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if requestHash != v.requestHash {
		return "", ErrConflict
	}
	return result, nil
}
func (s *Service) saveIdempotency(ctx context.Context, tx *sql.Tx, result string) error {
	v, ok := ctx.Value(idempotencyKey{}).(idempotencyRequest)
	if !ok {
		return nil
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO idempotency(actor,operation,key,request_hash,result,created_at)VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, v.actor, v.operation, v.key, v.requestHash, result, stamp(s.Now()))
	return e
}
