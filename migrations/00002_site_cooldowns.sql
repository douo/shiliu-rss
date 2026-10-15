-- +goose Up
CREATE TABLE site_cooldowns (host TEXT PRIMARY KEY, until TEXT NOT NULL);
-- +goose Down
DROP TABLE site_cooldowns;
