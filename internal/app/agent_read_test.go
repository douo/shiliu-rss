package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestReportReadFixedSummarizedMembersAndRetry(t *testing.T) {
	s, now := testService(t)
	ctx := context.Background()
	f := feedFixture(t, s)
	for i := 0; i < 63; i++ {
		addFixture(t, s, f, fmt.Sprintf("合成文章-%d", i))
	}
	// Archived unread articles must be included too.
	*now = now.AddDate(0, 0, 35)
	enableAgent(t, s)
	run, e := s.QueueAgentRun(ctx, "report", "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkAgentReportRead(ctx, run.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("queued report", e)
	}
	run, e = s.ClaimAgentRun(ctx)
	if e != nil {
		t.Fatal(e)
	}
	ms, e := s.AgentMembers(ctx, run.ID, "", "", 100, false)
	if e != nil {
		t.Fatal(e)
	}
	for i := range ms {
		ms[i].Depth, ms[i].State = "title", "summarized"
		ms[i].Card = []AgentBlock{{Kind: "fact", Text: "合成标题摘要"}}
		if i == 0 {
			ms[i].State = "reused"
		}
		if i == 1 {
			ms[i].State = "fallback"
		} // Actual excerpt/title facts count.
		if i == 60 {
			ms[i].State = "fallback"
			ms[i].Card = []AgentBlock{{Kind: "limitation", Text: "格式失败"}}
		}
		if i == 61 {
			ms[i].State = "unavailable"
			ms[i].Card = nil
		}
	}
	if e = s.SaveAgentMembers(ctx, run, ms[:62]); e != nil {
		t.Fatal(e)
	} // Last member remains pending.
	if _, e = s.MarkAgentReportRead(ctx, run.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("running report", e)
	}
	if e = s.SetAgentRunState(ctx, run, "partial", "budget", *now, nil); e != nil {
		t.Fatal(e)
	}
	later := addFixture(t, s, f, "快照之后的文章")
	already, _ := s.GetArticle(ctx, ms[0].ArticleID)
	read, favorite, note := true, true, "保留笔记"
	already, e = s.UpdateArticle(ctx, already.ID, already.Version, ArticlePatch{Read: &read, Favorite: &favorite, Note: &note})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := s.GetArticle(ctx, ms[1].ArticleID)
	run, e = s.GetAgentRun(ctx, run.ID)
	if e != nil || run.Result.Counts["summarized_articles"] != 60 || run.Result.Counts["unread_summarized"] != 59 || run.Result.Counts["pending"] != 1 {
		t.Fatal(run.Result.Counts, e)
	}
	// A mid-transaction failure must roll back the earlier article update and
	// its change record; the same request can then succeed in full.
	if _, e = s.Store.Write.Exec(`CREATE TRIGGER synthetic_read_failure BEFORE INSERT ON changes WHEN NEW.entity='article' AND NEW.entity_id='` + ms[2].ArticleID + `' BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.MarkAgentReportRead(ctx, run.ID); e == nil {
		t.Fatal("expected write failure")
	}
	rolledBack, e := s.GetArticle(ctx, before.ID)
	if e != nil || rolledBack.ReadAt != nil || rolledBack.Version != before.Version {
		t.Fatal("partial update committed", rolledBack, e)
	}
	if _, e = s.Store.Write.Exec(`DROP TRIGGER synthetic_read_failure`); e != nil {
		t.Fatal(e)
	}
	idem := WithIdempotency(ctx, "owner", "report-read", "synthetic-read-key", "fixed-request")
	out, e := s.MarkAgentReportRead(idem, run.ID)
	if e != nil || out.MarkedRead != 59 {
		t.Fatal(out, e)
	}
	after, _ := s.GetArticle(ctx, before.ID)
	if after.ReadAt == nil || !after.ReadAt.Equal(*now) || after.Version != before.Version+1 || after.ReadAtUnknown {
		t.Fatal("missing state/version", after)
	}
	kept, _ := s.GetArticle(ctx, already.ID)
	if kept.Version != already.Version || kept.Note != note || !kept.Favorite {
		t.Fatal("already read mutated", kept)
	}
	for _, articleID := range []string{ms[60].ArticleID, ms[61].ArticleID, ms[62].ArticleID, later.ID} {
		a, _ := s.GetArticle(ctx, articleID)
		if a.ReadAt != nil {
			t.Fatal("marked coverage gap/later arrival", articleID)
		}
	}
	// A lost-response retry returns the same result and cannot consume a later
	// explicit unread decision. A fresh action can mark that article again.
	read = false
	after, e = s.UpdateArticle(ctx, after.ID, after.Version, ArticlePatch{Read: &read})
	if e != nil {
		t.Fatal(e)
	}
	*now = now.Add(time.Hour)
	again, e := s.MarkAgentReportRead(idem, run.ID)
	if e != nil || again != out {
		t.Fatal(again, e)
	}
	still, _ := s.GetArticle(ctx, after.ID)
	if still.ReadAt != nil || still.Version != after.Version {
		t.Fatal("retry rewrote deliberate unread", still)
	}
	fresh, e := s.MarkAgentReportRead(ctx, run.ID)
	if e != nil || fresh.MarkedRead != 1 {
		t.Fatal(fresh, e)
	}
	if _, e = s.MarkAgentReportRead(ctx, "999999"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}

	// All article changes commit with the state. An authorization failure has
	// no side effects, even through the same business endpoint.
	deniedError := errors.New("synthetic revoked session")
	denied := WithAuthorization(ctx, func(context.Context, *sql.Tx) error { return deniedError })
	if _, e = s.MarkAgentReportRead(denied, run.ID); !errors.Is(e, deniedError) {
		t.Fatal(e)
	}
}
