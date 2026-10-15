-- +goose Up
CREATE TABLE categories (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL UNIQUE, version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE feeds (
 id INTEGER PRIMARY KEY AUTOINCREMENT, url TEXT NOT NULL UNIQUE, title TEXT NOT NULL, site_url TEXT NOT NULL DEFAULT '', category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
 paused INTEGER NOT NULL DEFAULT 0, archive INTEGER NOT NULL DEFAULT 0, interval_hours INTEGER NOT NULL DEFAULT 5, read_days INTEGER NOT NULL DEFAULT 7, unread_days INTEGER NOT NULL DEFAULT 30,
 etag TEXT NOT NULL DEFAULT '', last_modified TEXT NOT NULL DEFAULT '', last_attempt_at TEXT, last_success_at TEXT, next_check_at TEXT NOT NULL,
 error TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1, allow_host TEXT NOT NULL DEFAULT ''
);
CREATE TABLE articles (
 id INTEGER PRIMARY KEY AUTOINCREMENT, feed_id INTEGER NOT NULL REFERENCES feeds(id), title TEXT NOT NULL, url TEXT NOT NULL, author TEXT NOT NULL DEFAULT '', published_at TEXT,
 first_seen_at TEXT NOT NULL, read_at TEXT, favorite INTEGER NOT NULL DEFAULT 0, removed INTEGER NOT NULL DEFAULT 0,
 note TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '[]', version INTEGER NOT NULL DEFAULT 1,
 imported_at TEXT, protection_until TEXT, provenance TEXT NOT NULL DEFAULT 'feed', read_at_unknown INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX article_page ON articles(first_seen_at DESC,id DESC);
CREATE INDEX article_feed ON articles(feed_id);
CREATE TABLE identities (feed_id INTEGER NOT NULL REFERENCES feeds(id), kind TEXT NOT NULL, value TEXT NOT NULL, article_id INTEGER NOT NULL REFERENCES articles(id), PRIMARY KEY(feed_id,kind,value));
CREATE TABLE article_content (article_id INTEGER NOT NULL REFERENCES articles(id), kind TEXT NOT NULL CHECK(kind IN ('feed','fulltext')), html TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', hash TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'available', obtained_at TEXT NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(article_id,kind));
CREATE TABLE artifacts (id INTEGER PRIMARY KEY AUTOINCREMENT,article_id INTEGER NOT NULL REFERENCES articles(id),actor TEXT NOT NULL,external_id TEXT NOT NULL,kind TEXT NOT NULL,content TEXT NOT NULL,basis_hash TEXT NOT NULL,created_at TEXT NOT NULL,version INTEGER NOT NULL DEFAULT 1,UNIQUE(actor,external_id));
CREATE VIRTUAL TABLE search USING fts5(title,url,feed_content,fulltext,organization, content='',contentless_delete=1);
CREATE TABLE jobs (id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,target_id INTEGER NOT NULL,state TEXT NOT NULL DEFAULT 'queued',due_at TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,lease_owner TEXT NOT NULL DEFAULT '',lease_until TEXT,error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX active_job ON jobs(kind,target_id) WHERE state IN ('queued','running','waiting_retry');
CREATE INDEX job_due ON jobs(state,due_at);
CREATE TABLE changes (seq INTEGER PRIMARY KEY AUTOINCREMENT,entity TEXT NOT NULL,entity_id TEXT NOT NULL,kind TEXT NOT NULL,version INTEGER NOT NULL,at TEXT NOT NULL);
CREATE TABLE imports(source_instance TEXT NOT NULL,entity_type TEXT NOT NULL,source_id TEXT NOT NULL,target_id TEXT NOT NULL,imported_at TEXT NOT NULL,PRIMARY KEY(source_instance,entity_type,source_id));
CREATE TABLE account(id INTEGER PRIMARY KEY CHECK(id=1),username TEXT NOT NULL,password_hash TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE sessions(hash TEXT PRIMARY KEY,csrf TEXT NOT NULL,expires_at TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE tokens(id TEXT PRIMARY KEY,name TEXT NOT NULL,hash TEXT NOT NULL UNIQUE,scopes TEXT NOT NULL,created_at TEXT NOT NULL,revoked_at TEXT);
CREATE TABLE idempotency(actor TEXT NOT NULL,operation TEXT NOT NULL,key TEXT NOT NULL,request_hash TEXT NOT NULL,result TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(actor,operation,key));
-- +goose Down
DROP TABLE idempotency;
DROP TABLE tokens;
DROP TABLE sessions;
DROP TABLE account;
DROP TABLE imports;
DROP TABLE changes;
DROP TABLE jobs;
DROP TABLE search;
DROP TABLE artifacts;
DROP TABLE article_content;
DROP TABLE identities;
DROP TABLE articles;
DROP TABLE feeds;
DROP TABLE categories;
