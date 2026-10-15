package app

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type hostCooldownKey struct{}
type hostCooldown struct {
	host  string
	until time.Time
}

func cooldownHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// WithHostCooldown binds a rate-limit response to its failed task transaction.
// The runner checks HostCooldown before any feed or explicit fulltext request.
func WithHostCooldown(ctx context.Context, host string, until time.Time) context.Context {
	return context.WithValue(ctx, hostCooldownKey{}, hostCooldown{cooldownHost(host), until.UTC()})
}
func (s *Service) HostCooldown(ctx context.Context, host string) (time.Time, error) {
	var until string
	e := s.Store.Read.QueryRowContext(ctx, `SELECT until FROM site_cooldowns WHERE host=?`, cooldownHost(host)).Scan(&until)
	if e == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if e != nil {
		return time.Time{}, e
	}
	return parse(until), nil
}
func (s *Service) saveHostCooldown(ctx context.Context, tx *sql.Tx) error {
	v, ok := ctx.Value(hostCooldownKey{}).(hostCooldown)
	if !ok {
		return nil
	}
	if v.host == "" || v.until.IsZero() {
		return ErrInvalid
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO site_cooldowns(host,until)VALUES(?,?) ON CONFLICT(host) DO UPDATE SET until=max(site_cooldowns.until,excluded.until)`, v.host, stamp(v.until))
	return e
}
