package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func enableAgent(t *testing.T, s *Service) AgentSettings {
	t.Helper()
	v := DefaultAgentSettings()
	v.Enabled = true
	v.BaseURL = "http://localhost:1234/v1"
	v.Model = "synthetic"
	out, e := s.SaveAgentSettings(context.Background(), v, nil)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestAgentScheduleSnapshotLeaseAndBudget(t *testing.T) {
	s, now := testService(t)
	ctx := context.Background()
	if _, e := s.QueueAgentRun(ctx, "report", "{}", ""); !errors.Is(e, ErrAgentDisabled) {
		t.Fatal("unconfigured enabled", e)
	}
	f := feedFixture(t, s)
	a := addFixture(t, s, f, "旧的未读")
	*now = now.AddDate(0, 0, 35)
	v := enableAgent(t, s)
	if v.NextReportAt == nil || v.NextReportAt.Sub(*now) != 24*time.Hour {
		t.Fatal("next 08:00", v.NextReportAt, *now)
	}
	if e := s.ScheduleAgentDue(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ := s.ListAgentRuns(ctx, "report", "", 30)
	if len(runs) != 0 {
		t.Fatal("ran before slot")
	}
	*now = now.Add(25 * time.Hour)
	if e := s.ScheduleAgentDue(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.ScheduleAgentDue(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ = s.ListAgentRuns(ctx, "report", "", 30)
	if len(runs) != 1 || runs[0].Total != 1 {
		t.Fatal("snapshot excludes archive/duplicate schedule", runs)
	}
	addFixture(t, s, f, "快照后到达")
	ms, e := s.AgentMembers(ctx, runs[0].ID, "", "", 20, false)
	if e != nil || len(ms) != 1 || ms[0].ArticleID != a.ID {
		t.Fatal(ms, e)
	}
	claimed, e := s.ClaimAgentRun(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveAgentCall(ctx, claimed, v, "step", v.DailyTokens+1); !errors.Is(e, ErrAgentBudget) {
		t.Fatal("budget exceeded", e)
	}
	if _, e = s.ReserveAgentCall(ctx, claimed, v, "step", 1024); e != nil {
		t.Fatal(e)
	}
	usage, e := s.AgentUsage(ctx, v)
	if e != nil || usage.Tokens != 1024 || usage.UnknownCalls != 1 {
		t.Fatal(usage, e)
	}
	*now = now.Add(3 * time.Minute)
	newLease, e := s.ClaimAgentRun(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if newLease.Fence <= claimed.Fence {
		t.Fatal("no fence increment")
	}
	if e = s.SetAgentRunState(ctx, claimed, "complete", "", *now, nil); !errors.Is(e, ErrLease) {
		t.Fatal("stale lease committed", e)
	}
	v.Enabled = false
	if _, e = s.SaveAgentSettings(ctx, v, nil); e != nil {
		t.Fatal(e)
	}
	if e = s.RenewAgentRun(ctx, newLease); !errors.Is(e, ErrLease) {
		t.Fatal("disabled agent lease retained", e)
	}
}
func TestAgentAutomaticFulltextDoesNotReviveExpiredContent(t *testing.T) {
	s, now := testService(t)
	ctx := context.Background()
	f := feedFixture(t, s)
	f.Importance = "high"
	f.Paused = true
	f, e := s.UpdateFeed(ctx, f.ID, f.Version, f)
	if e != nil {
		t.Fatal(e)
	}
	a := addFixture(t, s, f, "重要文章")
	enableAgent(t, s)
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	run, e = s.ClaimAgentRun(ctx)
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.AgentFulltext(ctx, run, a.ID)
	if e != nil || job.ID == "" {
		t.Fatal("missing returned job", job, e)
	}
	j, e := s.ClaimJob(ctx, "test", time.Minute)
	if e != nil || j.ID != job.ID {
		t.Fatal(j, e)
	}
	*now = now.AddDate(0, 0, 91)
	// Restore the network job lease to represent a fetch completed at the boundary.
	_, e = s.Store.Write.Exec(`UPDATE jobs SET lease_until=? WHERE id=?`, stamp(now.Add(time.Minute)), j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteFulltextJob(ctx, j.ID, "test", FulltextResult{Text: "must not revive"}); e != nil {
		t.Fatal(e)
	}
	article, e := s.GetArticle(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range article.Content {
		if c.Kind == "fulltext" && c.State == "available" {
			t.Fatal("expired body revived")
		}
	}
}
func TestAgentConversationIdempotencyAndKeyRedaction(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	v := enableAgent(t, s)
	secret := "synthetic-secret"
	v, e := s.SaveAgentSettings(ctx, v, &secret)
	if e != nil || !v.KeyConfigured {
		t.Fatal(v, e)
	}
	v, e = s.SaveAgentSettings(ctx, v, nil)
	if e != nil || !v.KeyConfigured {
		t.Fatal("blank update erased secret", e)
	}
	key, e := s.AgentCredential(ctx)
	if e != nil || key != secret {
		t.Fatal("credential")
	}
	conv, e := s.CreateAgentConversation(ctx, "", "")
	if e != nil {
		t.Fatal(e)
	}
	idem := WithIdempotency(ctx, "test", "message", "repeat", "hash")
	r1, e := s.SendAgentMessage(idem, conv.ID, "hello")
	if e != nil {
		t.Fatal(e)
	}
	r2, e := s.SendAgentMessage(idem, conv.ID, "hello")
	if e != nil || r1.ID != r2.ID {
		t.Fatal("duplicate", r1, r2, e)
	}
	msgs, e := s.AgentMessages(ctx, conv.ID, "")
	if e != nil || len(msgs) != 1 {
		t.Fatal(msgs, e)
	}
	if _, e = s.SendAgentMessage(ctx, conv.ID, "another"); !errors.Is(e, ErrConflict) {
		t.Fatal("parallel chat accepted", e)
	}
}

func TestAgentRetrievalHonorsContentDeadlineAndReportCohort(t *testing.T) {
	s, now := testService(t)
	ctx := context.Background()
	f := feedFixture(t, s)
	a := addFixture(t, s, f, "合成甲")
	enableAgent(t, s)
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	b := addFixture(t, s, f, "合成乙")
	found, e := s.AgentSearch(ctx, "SQLite", run.ID, 12)
	if e != nil || len(found) != 1 || found[0].ID != a.ID {
		t.Fatal("wrong cohort", found, b.ID, e)
	}
	*now = now.AddDate(0, 0, 91)
	found, e = s.AgentSearch(ctx, "SQLite", "", 12)
	if e != nil || len(found) != 0 {
		t.Fatal("expired body still retrieved", len(found), e)
	}
	found, e = s.AgentSearch(ctx, "合成甲", "", 12)
	if e != nil || len(found) != 1 {
		t.Fatal("metadata missing", len(found), e)
	}
}

func TestAgentLargeSnapshotPaginationAndPartialAccounting(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	f := feedFixture(t, s)
	enableAgent(t, s)
	// Synthetic metadata, not a model-quality benchmark. A single insert builds a large cohort.
	_, e := s.Store.Write.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO articles(feed_id,title,url,first_seen_at) SELECT ?, 'synthetic-'||x,'https://example.invalid/'||x,? FROM n`, f.ID, stamp(s.Now().AddDate(0, 0, -40)))
	if e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil || run.Total != 10000 {
		t.Fatal(run.Total, e)
	}
	after := ""
	seen := map[string]bool{}
	for {
		page, e := s.AgentMembers(ctx, run.ID, after, "", 200, false)
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		for _, m := range page {
			if seen[m.ArticleID] {
				t.Fatal("duplicate")
			}
			seen[m.ArticleID] = true
		}
		after = page[len(page)-1].ArticleID
	}
	if len(seen) != 10000 {
		t.Fatal("missing members", len(seen))
	}
	counts, e := s.AgentCounts(ctx, run.ID)
	if e != nil || counts["pending"] != 10000 {
		t.Fatal(counts, e)
	}
	t.Logf("10,000 archived unread snapshot + 50 pages: %s", time.Since(started))
}
