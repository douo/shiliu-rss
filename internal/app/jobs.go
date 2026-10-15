package app

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"time"
)

const jobCols = `id,kind,target_id,state,due_at,attempts,lease_owner,lease_until,error,created_at,updated_at`

func scanJob(row scanner) (Job, error) {
	var j Job
	var due, created, updated string
	var lease sql.NullString
	e := row.Scan(&j.ID, &j.Kind, &j.TargetID, &j.State, &due, &j.Attempts, &j.LeaseOwner, &lease, &j.Error, &created, &updated)
	if e == sql.ErrNoRows {
		e = ErrNotFound
	}
	j.DueAt = parse(due)
	j.CreatedAt = parse(created)
	j.UpdatedAt = parse(updated)
	j.LeaseUntil = ptrTime(lease)
	return j, e
}
func (s *Service) GetJob(ctx context.Context, jobID string) (Job, error) {
	return scanJob(s.Store.Read.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=?`, jobID))
}
func (s *Service) queue(ctx context.Context, tx *sql.Tx, kind, target string) (Job, error) {
	saved, err := s.idempotentResult(ctx, tx)
	if err != nil {
		return Job{}, err
	}
	if saved != "" {
		return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=?`, saved))
	}
	now := stamp(s.Now())
	if _, e := tx.ExecContext(ctx, `INSERT INTO jobs(kind,target_id,due_at,created_at,updated_at)VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, kind, target, now, now, now); e != nil {
		return Job{}, e
	}
	j, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE kind=? AND target_id=? AND state IN ('queued','running','waiting_retry')`, kind, target))
	if err != nil {
		return j, err
	}
	return j, s.saveIdempotency(ctx, tx, j.ID)
}
func (s *Service) QueueFeed(ctx context.Context, feedID string) (Job, error) {
	var j Job
	e := s.write(ctx, func(tx *sql.Tx) error {
		var exists, paused int
		if e := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(paused),0) FROM feeds WHERE id=?`, feedID).Scan(&exists, &paused); e != nil {
			return e
		}
		if exists == 0 {
			return ErrNotFound
		}
		if paused > 0 {
			return ErrConflict
		}
		var e error
		j, e = s.queue(ctx, tx, "feed", feedID)
		return e
	})
	return j, e
}
func (s *Service) QueueFulltext(ctx context.Context, articleID string) (Job, error) {
	var j Job
	e := s.write(ctx, func(tx *sql.Tx) error {
		var exists int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM articles WHERE id=?`, articleID).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return ErrNotFound
		}
		var e error
		j, e = s.queue(ctx, tx, "fulltext", articleID)
		return e
	})
	return j, e
}

// ScheduleDue coalesces missed intervals and creates no historical catch-up jobs.
func (s *Service) ScheduleDue(ctx context.Context) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		now := stamp(s.Now())
		_, e := tx.ExecContext(ctx, `INSERT INTO jobs(kind,target_id,due_at,created_at,updated_at)SELECT 'feed',id,next_check_at,?,? FROM feeds WHERE paused=0 AND next_check_at<=? ON CONFLICT DO NOTHING`, now, now, now)
		return e
	})
}
func (s *Service) NextJobAt(ctx context.Context) (time.Time, error) {
	var next sql.NullString
	e := s.Store.Read.QueryRowContext(ctx, `SELECT min(at) FROM (SELECT min(due_at) AS at FROM jobs j WHERE state IN ('queued','waiting_retry') AND (kind<>'feed' OR EXISTS(SELECT 1 FROM feeds f WHERE f.id=j.target_id AND f.paused=0)) UNION ALL SELECT min(lease_until) FROM jobs WHERE state='running' UNION ALL SELECT min(next_check_at) FROM feeds f WHERE paused=0 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='feed' AND j.target_id=f.id AND j.state IN ('queued','running','waiting_retry')))`).Scan(&next)
	if !next.Valid {
		return s.Now().Add(5 * time.Minute), e
	}
	return parse(next.String), e
}
func (s *Service) ClaimJob(ctx context.Context, owner string, lease time.Duration) (Job, error) {
	var j Job
	if owner == "" || lease <= 0 {
		return j, ErrInvalid
	}
	e := s.write(ctx, func(tx *sql.Tx) error {
		now := stamp(s.Now())
		_, e := tx.ExecContext(ctx, `UPDATE jobs SET state='waiting_retry',lease_owner='',lease_until=NULL,due_at=?,updated_at=? WHERE state='running' AND lease_until<=?`, now, now, now)
		if e != nil {
			return e
		}
		var jobID string
		e = tx.QueryRowContext(ctx, `SELECT j.id FROM jobs j WHERE j.state IN ('queued','waiting_retry') AND j.due_at<=? AND (j.kind<>'feed' OR EXISTS(SELECT 1 FROM feeds f WHERE f.id=j.target_id AND f.paused=0)) ORDER BY j.due_at,j.id LIMIT 1`, now).Scan(&jobID)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE jobs SET state='running',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=? WHERE id=?`, owner, stamp(s.Now().Add(lease)), now, jobID)
		if e != nil {
			return e
		}
		j, e = scanJob(tx.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=?`, jobID))
		if e != nil {
			return e
		}
		if j.Kind == "feed" {
			_, e = tx.ExecContext(ctx, `UPDATE feeds SET last_attempt_at=? WHERE id=?`, now, j.TargetID)
		}
		return e
	})
	return j, e
}
func (s *Service) leasedJob(ctx context.Context, tx *sql.Tx, jobID, owner string) (Job, error) {
	j, e := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=?`, jobID))
	if e != nil {
		return j, e
	}
	if j.State != "running" || j.LeaseOwner != owner || j.LeaseUntil == nil || !s.Now().Before(*j.LeaseUntil) {
		return j, ErrLease
	}
	return j, nil
}
func (s *Service) finish(ctx context.Context, tx *sql.Tx, j Job) error {
	_, e := tx.ExecContext(ctx, `UPDATE jobs SET state='succeeded',lease_owner='',lease_until=NULL,error='',updated_at=? WHERE id=?`, stamp(s.Now()), j.ID)
	return e
}
func (s *Service) CompleteFeedJob(ctx context.Context, jobID, owner string, result FeedResult) error {
	if !result.NotModified {
		prepared, err := prepareEntries(result.Entries)
		if err != nil {
			return err
		}
		result.Entries = prepared
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		j, e := s.leasedJob(ctx, tx, jobID, owner)
		if e != nil {
			return e
		}
		if j.Kind != "feed" {
			return ErrInvalid
		}
		if !result.NotModified {
			for _, entry := range result.Entries {
				if _, e = s.upsertEntry(ctx, tx, j.TargetID, entry, nil); e != nil {
					return e
				}
			}
		}
		var interval int
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT interval_hours,version FROM feeds WHERE id=?`, j.TargetID).Scan(&interval, &version); e != nil {
			return e
		}
		spread := time.Duration(interval) * time.Hour / 5
		next := s.Now().Add(time.Duration(interval)*time.Hour - spread + time.Duration(rand.Int64N(int64(2*spread)+1)))
		_, e = tx.ExecContext(ctx, `UPDATE feeds SET title=CASE WHEN ?='' THEN title ELSE ? END,site_url=CASE WHEN ?='' THEN site_url ELSE ? END,etag=CASE WHEN ? AND ?='' THEN etag ELSE ? END,last_modified=CASE WHEN ? AND ?='' THEN last_modified ELSE ? END,last_success_at=?,next_check_at=?,error='',version=version+1 WHERE id=?`, result.Title, result.Title, result.SiteURL, result.SiteURL, result.NotModified, result.ETag, result.ETag, result.NotModified, result.LastModified, result.LastModified, stamp(s.Now()), stamp(next), j.TargetID)
		if e != nil {
			return e
		}
		if e = s.change(ctx, tx, "feed", j.TargetID, "checked", version+1); e != nil {
			return e
		}
		return s.finish(ctx, tx, j)
	})
}
func (s *Service) CompleteFulltextJob(ctx context.Context, jobID, owner string, result FulltextResult) error {
	if len(result.HTML) > 8<<20 || len(result.Text) > 8<<20 {
		return ErrInvalid
	}
	if result.HTML != "" {
		result.HTML, result.Text = CleanContent(result.HTML)
	}
	if result.HTML == "" && result.Text == "" {
		return ErrInvalid
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		j, e := s.leasedJob(ctx, tx, jobID, owner)
		if e != nil {
			return e
		}
		if j.Kind != "fulltext" {
			return ErrInvalid
		}
		var seen string
		var hadPurged int
		if e = tx.QueryRowContext(ctx, `SELECT first_seen_at,EXISTS(SELECT 1 FROM article_content WHERE article_id=articles.id AND (state='purged' OR expires_at<=?)) FROM articles WHERE id=?`, stamp(s.Now()), j.TargetID).Scan(&seen, &hadPurged); e != nil {
			return e
		}
		expiry := parse(seen).AddDate(0, 0, 90)
		if hadPurged > 0 || !s.Now().Before(expiry) {
			expiry = s.Now().AddDate(0, 0, 90)
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO article_content(article_id,kind,html,text,hash,state,obtained_at,expires_at)VALUES(?,'fulltext',?,?,?,'available',?,?) ON CONFLICT(article_id,kind) DO UPDATE SET html=excluded.html,text=excluded.text,hash=excluded.hash,state='available',obtained_at=excluded.obtained_at,expires_at=excluded.expires_at`, j.TargetID, result.HTML, result.Text, hash(result.HTML+"\x00"+result.Text), stamp(s.Now()), stamp(expiry))
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE articles SET version=version+1 WHERE id=?`, j.TargetID); e != nil {
			return e
		}
		if e = s.indexArticle(ctx, tx, j.TargetID); e != nil {
			return e
		}
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT version FROM articles WHERE id=?`, j.TargetID).Scan(&version); e != nil {
			return e
		}
		if e = s.change(ctx, tx, "article", j.TargetID, "fulltext", version); e != nil {
			return e
		}
		return s.finish(ctx, tx, j)
	})
}
func (s *Service) FailJob(ctx context.Context, jobID, owner, reason string, retryAfter *time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		j, e := s.leasedJob(ctx, tx, jobID, owner)
		if e != nil {
			return e
		}
		if e = s.saveHostCooldown(ctx, tx); e != nil {
			return e
		}
		delays := []time.Duration{15 * time.Minute, time.Hour, 4 * time.Hour, 6 * time.Hour}
		index := j.Attempts - 1
		if index >= len(delays) {
			index = len(delays) - 1
		}
		delay := delays[index]
		next := s.Now().Add(delay + time.Duration(rand.Int64N(int64(delay/10)+1)))
		if retryAfter != nil && retryAfter.After(next) {
			next = *retryAfter
		}
		state := "waiting_retry"
		if j.Kind == "fulltext" && j.Attempts >= 3 {
			state = "failed"
		}
		if len(reason) > 200 {
			reason = reason[:200]
		}
		_, e = tx.ExecContext(ctx, `UPDATE jobs SET state=?,due_at=?,lease_owner='',lease_until=NULL,error=?,updated_at=? WHERE id=?`, state, stamp(next), reason, stamp(s.Now()), j.ID)
		if e != nil {
			return e
		}
		if j.Kind == "feed" {
			_, e = tx.ExecContext(ctx, `UPDATE feeds SET error=?,next_check_at=? WHERE id=?`, reason, stamp(next), j.TargetID)
		}
		return e
	})
}

// DeferJob returns a claimed task to the queue when an outbound concurrency
// gate is busy. Since no network attempt occurred, attempts is restored.
func (s *Service) DeferJob(ctx context.Context, jobID, owner string, due time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		j, e := s.leasedJob(ctx, tx, jobID, owner)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE jobs SET state='queued',due_at=?,attempts=max(0,attempts-1),lease_owner='',lease_until=NULL,updated_at=? WHERE id=?`, stamp(due), stamp(s.Now()), j.ID)
		return e
	})
}
