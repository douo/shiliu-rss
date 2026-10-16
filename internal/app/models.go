package app

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrVersion  = errors.New("version mismatch")
	ErrInvalid  = errors.New("invalid input")
	ErrLease    = errors.New("job lease lost")
)

type Category struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}
type Feed struct {
	ID            string     `json:"id"`
	URL           string     `json:"url"`
	Title         string     `json:"title"`
	SiteURL       string     `json:"site_url"`
	CategoryID    string     `json:"category_id"`
	Importance    string     `json:"importance"`
	Paused        bool       `json:"paused"`
	Archive       bool       `json:"archive"`
	IntervalHours int        `json:"interval_hours"`
	ReadDays      int        `json:"read_days"`
	UnreadDays    int        `json:"unread_days"`
	UnreadCount   int64      `json:"unread_count"`
	ETag          string     `json:"-"`
	LastModified  string     `json:"-"`
	AllowHost     string     `json:"allow_host"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	NextCheckAt   time.Time  `json:"next_check_at"`
	Error         string     `json:"error"`
	Version       int64      `json:"version"`
}
type Content struct {
	Kind       string     `json:"kind"`
	HTML       string     `json:"html"`
	Text       string     `json:"text"`
	Hash       string     `json:"hash"`
	State      string     `json:"state"`
	ObtainedAt time.Time  `json:"obtained_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}
type Article struct {
	ID               string     `json:"id"`
	FeedID           string     `json:"feed_id"`
	FeedTitle        string     `json:"feed_title"`
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	Author           string     `json:"author"`
	PublishedAt      *time.Time `json:"published_at"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	ReadAt           *time.Time `json:"read_at"`
	Favorite         bool       `json:"favorite"`
	Hidden           bool       `json:"hidden"`
	Removed          bool       `json:"removed"`
	Note             string     `json:"note"`
	Tags             []string   `json:"tags"`
	Version          int64      `json:"version"`
	BodyState        string     `json:"body_state"`
	ContentHash      string     `json:"content_hash"`
	ContentExpiresAt *time.Time `json:"content_expires_at"`
	Content          []Content  `json:"content,omitempty"`
	Artifacts        []Artifact `json:"artifacts,omitempty"`
	ImportedAt       *time.Time `json:"imported_at"`
	ProtectionUntil  *time.Time `json:"protection_until"`
	Provenance       string     `json:"provenance"`
	ReadAtUnknown    bool       `json:"read_at_unknown"`
}
type ArticlePatch struct {
	Read     *bool     `json:"read,omitempty"`
	Favorite *bool     `json:"favorite,omitempty"`
	Note     *string   `json:"note,omitempty"`
	Tags     *[]string `json:"tags,omitempty"`
}
type ArticleQuery struct {
	Scope      string
	FeedID     string
	CategoryID string
	Read       *bool
	Favorite   *bool
	Query      string
	Cursor     string
	Limit      int
	From       *time.Time
	Until      *time.Time
}
type ArticlePage struct {
	Items         []Article `json:"items"`
	NextCursor    string    `json:"next_cursor"`
	HighWatermark string    `json:"high_watermark"`
}
type Artifact struct {
	ID         string    `json:"id"`
	ArticleID  string    `json:"article_id"`
	Actor      string    `json:"actor"`
	ExternalID string    `json:"external_id"`
	Kind       string    `json:"kind"`
	Content    string    `json:"content"`
	BasisHash  string    `json:"basis_hash"`
	CreatedAt  time.Time `json:"created_at"`
	Version    int64     `json:"version"`
}
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	TargetID   string     `json:"target_id"`
	State      string     `json:"state"`
	DueAt      time.Time  `json:"due_at"`
	Attempts   int        `json:"attempts"`
	LeaseOwner string     `json:"-"`
	LeaseUntil *time.Time `json:"lease_until"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
type Change struct {
	Seq      string    `json:"seq"`
	Entity   string    `json:"entity"`
	EntityID string    `json:"entity_id"`
	Kind     string    `json:"kind"`
	Version  int64     `json:"version"`
	At       time.Time `json:"at"`
}
type ChangePage struct {
	Items         []Change `json:"items"`
	NextCursor    string   `json:"next_cursor"`
	HighWatermark string   `json:"high_watermark"`
}
type FeedEntry struct {
	GUID        string
	URL         string
	Title       string
	Author      string
	PublishedAt *time.Time
	HTML        string
	Text        string
	Aliases     map[string][]string
}
type FeedResult struct {
	Title        string
	SiteURL      string
	ETag         string
	LastModified string
	NotModified  bool
	Entries      []FeedEntry
}
type FulltextResult struct {
	HTML string
	Text string
}
type RetentionPreview struct {
	Articles int   `json:"articles"`
	Contents int   `json:"contents"`
	Bytes    int64 `json:"bytes"`
}
type ImportArticleInput struct {
	SourceInstance string
	SourceID       string
	FeedID         string
	Entry          FeedEntry
	FirstSeenAt    *time.Time
	ReadAt         *time.Time
	Read           bool
	Favorite       bool
	Removed        bool
	OldHash        string
	Tags           []string
}
