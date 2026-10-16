-- +goose Up
CREATE INDEX article_published_page ON articles(COALESCE(published_at,first_seen_at) DESC,id DESC);
CREATE INDEX article_feed_published_page ON articles(feed_id,COALESCE(published_at,first_seen_at) DESC,id DESC);
CREATE INDEX article_feed_unread ON articles(feed_id) WHERE read_at IS NULL AND removed=0;

-- +goose Down
DROP INDEX article_feed_unread;
DROP INDEX article_feed_published_page;
DROP INDEX article_published_page;
