package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"shiliu-rss/api"
	"shiliu-rss/internal/app"
)

type Scheduler interface {
	Wake()
	Status() (time.Time, *time.Time, time.Time, bool)
}
type Server struct {
	App          *app.Service
	Origin       string
	CookieSecure bool
	Scheduler    Scheduler
	Assets       fs.FS
	login        loginGuard
}

func (s *Server) Handler() http.Handler {
	strict := api.NewStrictHandlerWithOptions(s, []api.StrictMiddlewareFunc{s.middleware}, api.StrictHTTPServerOptions{RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, e error) { writeProblem(w, r, app.ErrInvalid) }, ResponseErrorHandlerFunc: writeProblem})
	mux := http.NewServeMux()
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, e error) { writeProblem(w, r, app.ErrInvalid) }})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		good := s.App.Ping(ctx) == nil
		if s.Scheduler != nil {
			_, _, _, ok := s.Scheduler.Status()
			good = good && ok
		}
		w.Header().Set("Content-Type", "application/json")
		if !good {
			w.WriteHeader(503)
			io.WriteString(w, `{"status":"unavailable"}`)
			return
		}
		io.WriteString(w, `{"status":"ready"}`)
	})
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", random())
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: http: data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Shiliu-Content-Trust", "external-reference")
		r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "api" || strings.HasPrefix(path, "api/") {
		http.NotFound(w, r)
		return
	}
	if s.Assets == nil {
		http.NotFound(w, r)
		return
	}
	if path == "" {
		path = "200.html"
	}
	f, e := s.Assets.Open(path)
	if e != nil {
		if strings.Contains(path, ".") || strings.HasPrefix(path, "_app/") {
			http.NotFound(w, r)
			return
		}
		f, e = s.Assets.Open("200.html")
	}
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	reader, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "asset unavailable", 500)
		return
	}
	if strings.HasPrefix(path, "_app/immutable/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	name := st.Name()
	http.ServeContent(w, r, name, time.Time{}, reader)
}
func (s *Server) wake() {
	if s.Scheduler != nil {
		s.Scheduler.Wake()
	}
}
func val[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func (s *Server) ListArticles(ctx context.Context, r api.ListArticlesRequestObject) (api.ListArticlesResponseObject, error) {
	p := r.Params
	if p.Scope != nil && !p.Scope.Valid() {
		return nil, app.ErrInvalid
	}
	v, e := s.App.ListArticles(ctx, app.ArticleQuery{Scope: string(val(p.Scope)), FeedID: val(p.FeedId), CategoryID: val(p.CategoryId), Query: val(p.Q), Read: p.Read, Favorite: p.Favorite, From: p.From, Until: p.Until, Cursor: val(p.Cursor), Limit: val(p.Limit)})
	if e != nil {
		return nil, e
	}
	return convert[api.ListArticles200JSONResponse](v)
}
func (s *Server) GetArticle(ctx context.Context, r api.GetArticleRequestObject) (api.GetArticleResponseObject, error) {
	v, e := s.App.GetArticle(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	return convert[api.GetArticle200JSONResponse](v)
}
func (s *Server) UpdateArticle(ctx context.Context, r api.UpdateArticleRequestObject) (api.UpdateArticleResponseObject, error) {
	if r.Body == nil || r.Body.Version < 1 {
		return nil, app.ErrInvalid
	}
	p := r.Body
	v, e := s.App.UpdateArticle(ctx, r.Id, p.Version, app.ArticlePatch{Read: p.Read, Favorite: p.Favorite, Note: p.Note, Tags: p.Tags})
	if e != nil {
		return nil, e
	}
	return convert[api.UpdateArticle200JSONResponse](v)
}
func (s *Server) BulkRead(ctx context.Context, r api.BulkReadRequestObject) (api.BulkReadResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	if e := s.App.BulkRead(ctx, r.Body.Ids, r.Body.Read); e != nil {
		return nil, e
	}
	return api.BulkRead204Response{}, nil
}
func idempotent(ctx context.Context, key, operation string, body any) (context.Context, error) {
	if key == "" || len(key) > 128 {
		return ctx, app.ErrInvalid
	}
	b, e := json.Marshal(body)
	if e != nil {
		return ctx, e
	}
	return app.WithIdempotency(ctx, getPrincipal(ctx).Actor, operation, key, digest(string(b))), nil
}
func (s *Server) PutArtifact(ctx context.Context, r api.PutArtifactRequestObject) (api.PutArtifactResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "artifact:"+r.Id, r.Body)
	if e != nil {
		return nil, e
	}
	p := r.Body
	v, e := s.App.PutArtifact(ctx, app.Artifact{ArticleID: r.Id, Actor: getPrincipal(ctx).Actor, ExternalID: p.ExternalId, Kind: p.Kind, Content: p.Content, BasisHash: p.BasisHash})
	if e != nil {
		return nil, e
	}
	return convert[api.PutArtifact201JSONResponse](v)
}
func (s *Server) RequestFulltext(ctx context.Context, r api.RequestFulltextRequestObject) (api.RequestFulltextResponseObject, error) {
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "fulltext", r.Id)
	if e != nil {
		return nil, e
	}
	v, e := s.App.QueueFulltext(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	s.wake()
	return convert[api.RequestFulltext202JSONResponse](v)
}
func (s *Server) GetJob(ctx context.Context, r api.GetJobRequestObject) (api.GetJobResponseObject, error) {
	v, e := s.App.GetJob(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	return convert[api.GetJob200JSONResponse](v)
}
func (s *Server) ListChanges(ctx context.Context, r api.ListChangesRequestObject) (api.ListChangesResponseObject, error) {
	v, e := s.App.Changes(ctx, val(r.Params.Cursor), val(r.Params.Limit))
	if e != nil {
		return nil, e
	}
	return convert[api.ListChanges200JSONResponse](v)
}
func (s *Server) ListFeeds(ctx context.Context, r api.ListFeedsRequestObject) (api.ListFeedsResponseObject, error) {
	v, e := s.App.ListFeeds(ctx)
	if e != nil {
		return nil, e
	}
	return convert[api.ListFeeds200JSONResponse](v)
}
func feedInput(p api.FeedInput, base app.Feed) app.Feed {
	base.Title = p.Title
	base.URL = p.Url
	if p.Archive != nil {
		base.Archive = *p.Archive
	}
	if p.Paused != nil {
		base.Paused = *p.Paused
	}
	if p.CategoryId != nil {
		base.CategoryID = *p.CategoryId
	}
	if p.SiteUrl != nil {
		base.SiteURL = *p.SiteUrl
	}
	if p.AllowHost != nil {
		base.AllowHost = *p.AllowHost
	}
	if p.IntervalHours != nil {
		base.IntervalHours = int(*p.IntervalHours)
	}
	if p.ReadDays != nil {
		base.ReadDays = int(*p.ReadDays)
	}
	if p.UnreadDays != nil {
		base.UnreadDays = int(*p.UnreadDays)
	}
	return base
}
func (s *Server) CreateFeed(ctx context.Context, r api.CreateFeedRequestObject) (api.CreateFeedResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	v, e := s.App.CreateFeed(ctx, feedInput(*r.Body, app.Feed{}))
	if e != nil {
		return nil, e
	}
	s.wake()
	return convert[api.CreateFeed201JSONResponse](v)
}
func (s *Server) UpdateFeed(ctx context.Context, r api.UpdateFeedRequestObject) (api.UpdateFeedResponseObject, error) {
	if r.Body == nil || r.Body.Version == nil || *r.Body.Version < 1 {
		return nil, app.ErrInvalid
	}
	old, e := s.App.GetFeed(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	v, e := s.App.UpdateFeed(ctx, r.Id, *r.Body.Version, feedInput(*r.Body, old))
	if e != nil {
		return nil, e
	}
	s.wake()
	return convert[api.UpdateFeed200JSONResponse](v)
}
func (s *Server) DeleteFeed(ctx context.Context, r api.DeleteFeedRequestObject) (api.DeleteFeedResponseObject, error) {
	if e := s.App.DeleteFeed(ctx, r.Id, r.Params.Version); e != nil {
		return nil, e
	}
	s.wake()
	return api.DeleteFeed204Response{}, nil
}
func (s *Server) RefreshFeed(ctx context.Context, r api.RefreshFeedRequestObject) (api.RefreshFeedResponseObject, error) {
	ctx, e := idempotent(ctx, r.Params.IdempotencyKey, "refresh", r.Id)
	if e != nil {
		return nil, e
	}
	f, e := s.App.GetFeed(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	if f.Paused {
		return nil, app.ErrConflict
	}
	v, e := s.App.QueueFeed(ctx, r.Id)
	if e != nil {
		return nil, e
	}
	s.wake()
	return convert[api.RefreshFeed202JSONResponse](v)
}
func (s *Server) ListCategories(ctx context.Context, r api.ListCategoriesRequestObject) (api.ListCategoriesResponseObject, error) {
	v, e := s.App.ListCategories(ctx)
	if e != nil {
		return nil, e
	}
	return convert[api.ListCategories200JSONResponse](v)
}
func (s *Server) CreateCategory(ctx context.Context, r api.CreateCategoryRequestObject) (api.CreateCategoryResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	v, e := s.App.CreateCategory(ctx, r.Body.Title)
	if e != nil {
		return nil, e
	}
	return convert[api.CreateCategory201JSONResponse](v)
}
func (s *Server) UpdateCategory(ctx context.Context, r api.UpdateCategoryRequestObject) (api.UpdateCategoryResponseObject, error) {
	if r.Body == nil {
		return nil, app.ErrInvalid
	}
	v, e := s.App.UpdateCategory(ctx, r.Id, r.Body.Version, r.Body.Title)
	if e != nil {
		return nil, e
	}
	return convert[api.UpdateCategory200JSONResponse](v)
}
func (s *Server) DeleteCategory(ctx context.Context, r api.DeleteCategoryRequestObject) (api.DeleteCategoryResponseObject, error) {
	if e := s.App.DeleteCategory(ctx, r.Id, r.Params.Version); e != nil {
		return nil, e
	}
	return api.DeleteCategory204Response{}, nil
}
func (s *Server) PreviewRetention(ctx context.Context, r api.PreviewRetentionRequestObject) (api.PreviewRetentionResponseObject, error) {
	var f []app.Feed
	if r.Body != nil && r.Body.Feed != nil {
		old, e := s.App.GetFeed(ctx, val(r.Body.FeedId))
		if e != nil {
			return nil, e
		}
		f = append(f, feedInput(*r.Body.Feed, old))
	}
	v, e := s.App.RetentionPreview(ctx, f...)
	if e != nil {
		return nil, e
	}
	return convert[api.PreviewRetention200JSONResponse](v)
}
func (s *Server) GetHealth(ctx context.Context, r api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	_, e := s.App.Stats(ctx)
	var vQueued, vFailed int64
	if e != nil {
		return nil, e
	}
	if e = s.App.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE state IN ('queued','running','waiting_retry')`).Scan(&vQueued); e != nil {
		return nil, e
	}
	if e = s.App.Store.Read.QueryRowContext(ctx, `SELECT count(*) FROM feeds WHERE error<>''`).Scan(&vFailed); e != nil {
		return nil, e
	}
	out := api.GetHealth200JSONResponse{Status: "ready", QueuedJobs: vQueued, FailedFeeds: vFailed, SchedulerAt: s.App.Now(), NextCleanupAt: s.App.Now()}
	if s.Scheduler != nil {
		var good bool
		out.SchedulerAt, out.LastCleanupAt, out.NextCleanupAt, good = s.Scheduler.Status()
		if !good {
			out.Status = "degraded"
		}
	}
	return out, nil
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Outlines []outline `xml:"outline"`
}
type opml struct {
	XMLName  xml.Name  `xml:"opml"`
	Version  string    `xml:"version,attr"`
	Title    string    `xml:"head>title"`
	Outlines []outline `xml:"body>outline"`
}
type opmlResponse []byte

func (v opmlResponse) VisitExportOPMLResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="shiliu.opml"`)
	_, e := w.Write(v)
	return e
}
func (s *Server) ExportOPML(ctx context.Context, r api.ExportOPMLRequestObject) (api.ExportOPMLResponseObject, error) {
	feeds, e := s.App.ListFeeds(ctx)
	if e != nil {
		return nil, e
	}
	cats, e := s.App.ListCategories(ctx)
	if e != nil {
		return nil, e
	}
	doc := opml{Version: "2.0", Title: "拾流 RSS"}
	for _, c := range cats {
		group := outline{Text: c.Title}
		for _, f := range feeds {
			if f.CategoryID == c.ID {
				group.Outlines = append(group.Outlines, outline{Text: f.Title, Type: "rss", XMLURL: f.URL, HTMLURL: f.SiteURL})
			}
		}
		doc.Outlines = append(doc.Outlines, group)
	}
	for _, f := range feeds {
		if f.CategoryID == "" {
			doc.Outlines = append(doc.Outlines, outline{Text: f.Title, Type: "rss", XMLURL: f.URL, HTMLURL: f.SiteURL})
		}
	}
	data, e := xml.MarshalIndent(doc, "", "  ")
	return opmlResponse(append([]byte(xml.Header), data...)), e
}
func (s *Server) ImportOPML(ctx context.Context, r api.ImportOPMLRequestObject) (api.ImportOPMLResponseObject, error) {
	if r.Body == nil || len(r.Body.Xml) > 2<<20 {
		return nil, app.ErrInvalid
	}
	var doc opml
	if xml.Unmarshal([]byte(r.Body.Xml), &doc) != nil {
		return nil, app.ErrInvalid
	}
	existing, e := s.App.ListFeeds(ctx)
	if e != nil {
		return nil, e
	}
	urls := map[string]bool{}
	for _, f := range existing {
		urls[f.URL] = true
	}
	cats, e := s.App.ListCategories(ctx)
	if e != nil {
		return nil, e
	}
	categories := map[string]string{}
	for _, c := range cats {
		categories[c.Title] = c.ID
	}
	count, nodes := int64(0), 0
	var visit func([]outline, string, int) error
	visit = func(list []outline, cat string, depth int) error {
		if depth > 20 {
			return app.ErrInvalid
		}
		for _, o := range list {
			nodes++
			if nodes > 2000 {
				return app.ErrInvalid
			}
			title := o.Text
			if title == "" {
				title = o.Title
			}
			childCat := cat
			if o.XMLURL != "" {
				if !urls[o.XMLURL] {
					_, e := s.App.CreateFeed(ctx, app.Feed{Title: title, URL: o.XMLURL, SiteURL: o.HTMLURL, CategoryID: cat})
					if e != nil {
						return e
					}
					urls[o.XMLURL] = true
					count++
				}
			} else if title != "" {
				childCat = categories[title]
				if childCat == "" {
					c, e := s.App.CreateCategory(ctx, title)
					if e != nil {
						return e
					}
					childCat = c.ID
					categories[title] = c.ID
				}
			}
			if e := visit(o.Outlines, childCat, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	if e := visit(doc.Outlines, "", 0); e != nil {
		return nil, e
	}
	s.wake()
	return api.ImportOPML200JSONResponse{Imported: count}, nil
}
