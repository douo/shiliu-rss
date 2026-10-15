-- +goose Up
ALTER TABLE feeds ADD COLUMN importance TEXT NOT NULL DEFAULT 'normal' CHECK(importance IN ('high','normal','low'));
ALTER TABLE jobs ADD COLUMN reason TEXT NOT NULL DEFAULT '';
CREATE TABLE agent_settings (id INTEGER PRIMARY KEY CHECK(id=1), config TEXT NOT NULL, api_key TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1, next_report_at TEXT);
CREATE TABLE agent_runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL CHECK(kind IN ('report','chat','query')), state TEXT NOT NULL DEFAULT 'queued',
 schedule_key TEXT UNIQUE, config_version INTEGER NOT NULL, cutoff TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 due_at TEXT NOT NULL, lease_until TEXT, fence INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
 input TEXT NOT NULL DEFAULT '{}', result TEXT NOT NULL DEFAULT '{}', conversation_id INTEGER,
 total INTEGER NOT NULL DEFAULT 0, completed INTEGER NOT NULL DEFAULT 0, high_watermark TEXT NOT NULL DEFAULT '0'
);
CREATE INDEX agent_run_due ON agent_runs(state,due_at);
CREATE TABLE agent_steps (
 id INTEGER PRIMARY KEY AUTOINCREMENT, run_id INTEGER NOT NULL REFERENCES agent_runs(id), step_key TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'reserved', day TEXT NOT NULL, reserved_tokens INTEGER NOT NULL, used_tokens INTEGER NOT NULL DEFAULT 0,
 result TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE INDEX agent_step_day ON agent_steps(day);
CREATE INDEX agent_step_cache ON agent_steps(step_key,state);
CREATE TABLE report_members (
 run_id INTEGER NOT NULL REFERENCES agent_runs(id), article_id INTEGER NOT NULL REFERENCES articles(id),
 feed_id INTEGER NOT NULL, feed_title TEXT NOT NULL, category_id TEXT NOT NULL, category_title TEXT NOT NULL,
 importance TEXT NOT NULL, title TEXT NOT NULL, snapshot_hash TEXT NOT NULL DEFAULT '', basis_hash TEXT NOT NULL DEFAULT '', depth TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending', card TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', cache_key TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(run_id,article_id)
);
CREATE INDEX report_member_state ON report_members(run_id,state,feed_id,article_id);
CREATE INDEX report_member_cache ON report_members(cache_key,state);
CREATE TABLE agent_conversations (id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT NOT NULL,report_id INTEGER REFERENCES agent_runs(id),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE agent_messages (id INTEGER PRIMARY KEY AUTOINCREMENT,conversation_id INTEGER NOT NULL REFERENCES agent_conversations(id),role TEXT NOT NULL,text TEXT NOT NULL DEFAULT '',result TEXT NOT NULL DEFAULT '{}',run_id INTEGER REFERENCES agent_runs(id),created_at TEXT NOT NULL);
CREATE INDEX agent_message_page ON agent_messages(conversation_id,id);
-- +goose Down
DROP TABLE agent_messages;
DROP TABLE agent_conversations;
DROP TABLE report_members;
DROP TABLE agent_steps;
DROP TABLE agent_runs;
DROP TABLE agent_settings;
ALTER TABLE jobs DROP COLUMN reason;
ALTER TABLE feeds DROP COLUMN importance;
