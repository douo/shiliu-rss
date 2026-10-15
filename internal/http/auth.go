package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"shiliu-rss/api"
	"shiliu-rss/internal/app"
)

type authError struct {
	status int
	code   string
}

func (e authError) Error() string { return e.code }

var unauthorized = authError{401, "unauthorized"}
var forbidden = authError{403, "forbidden"}

func random() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(v string) string  { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func date(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// Hash parameters bound memory and concurrency; passwords never appear in logs.
func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", app.ErrInvalid
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	h := argon2.IDKey([]byte(password), salt, 2, 64*1024, 1, 32)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(h), nil
}
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[2])
	if e != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
func Initialize(ctx context.Context, s *app.Service, password string) error {
	h, e := HashPassword(password)
	if e != nil {
		return e
	}
	_, e = s.Store.Write.ExecContext(ctx, `INSERT INTO account(id,username,password_hash,created_at)VALUES(1,'owner',?,?)`, h, date(s.Now()))
	return e
}

type principal struct {
	Actor, Hash, CSRF string
	Owner             bool
	Scopes            map[string]bool
}
type requestKey struct{}
type principalKey struct{}
type exchange struct {
	w http.ResponseWriter
	r *http.Request
}

func getPrincipal(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p
}
func (s *Server) authenticate(r *http.Request) (principal, error) {
	ctx := r.Context()
	p := principal{Scopes: map[string]bool{}}
	if h := r.Header.Get("Authorization"); h != "" {
		if !strings.HasPrefix(h, "Bearer ") {
			return p, unauthorized
		}
		p.Hash = digest(strings.TrimPrefix(h, "Bearer "))
		var scopes string
		var revoked sql.NullString
		e := s.App.Store.Read.QueryRowContext(ctx, `SELECT id,scopes,revoked_at FROM tokens WHERE hash=?`, p.Hash).Scan(&p.Actor, &scopes, &revoked)
		if e != nil || revoked.Valid {
			return p, unauthorized
		}
		var list []string
		if json.Unmarshal([]byte(scopes), &list) != nil {
			return p, unauthorized
		}
		for _, v := range list {
			p.Scopes[v] = true
		}
		p.Actor = "token:" + p.Actor
		return p, nil
	}
	cookie, e := r.Cookie("shiliu_session")
	if e != nil {
		return p, unauthorized
	}
	p.Hash = digest(cookie.Value)
	var expiry string
	e = s.App.Store.Read.QueryRowContext(ctx, `SELECT csrf,expires_at FROM sessions WHERE hash=?`, p.Hash).Scan(&p.CSRF, &expiry)
	if e != nil || expiry <= date(s.App.Now()) {
		return p, unauthorized
	}
	p.Owner = true
	p.Actor = "owner"
	return p, nil
}
func (s *Server) checkCredential(p principal) func(context.Context, *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		var n int
		var e error
		if p.Owner {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE hash=? AND expires_at>?`, p.Hash, date(s.App.Now())).Scan(&n)
		} else {
			e = tx.QueryRowContext(ctx, `SELECT count(*) FROM tokens WHERE hash=? AND revoked_at IS NULL`, p.Hash).Scan(&n)
		}
		if e != nil {
			return e
		}
		if n != 1 {
			return unauthorized
		}
		return nil
	}
}
func (s *Server) middleware(next api.StrictHandlerFunc, operation string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = context.WithValue(ctx, requestKey{}, exchange{w, r})
		if operation == "Login" {
			if r.Header.Get("Origin") != s.Origin {
				return nil, forbidden
			}
			return next(ctx, w, r, request)
		}
		p, e := s.authenticate(r)
		if operation == "GetSession" {
			if e == nil && p.Owner {
				ctx = context.WithValue(ctx, principalKey{}, p)
			}
			return next(ctx, w, r, request)
		}
		if e != nil {
			return nil, e
		}
		if !p.Owner && !strings.HasPrefix(operation, "Agent") {
			scope := "owner"
			switch operation {
			case "ListArticles", "GetArticle", "ListFeeds", "ListCategories", "ListChanges", "GetJob":
				scope = "articles:read"
			case "UpdateArticle", "BulkRead":
				scope = "reading:write"
			case "PutArtifact":
				scope = "artifacts:write"
			case "RequestFulltext":
				scope = "fulltext:request"
			}
			if !p.Scopes[scope] {
				return nil, forbidden
			}
		}
		if p.Owner && r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != s.Origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(p.CSRF)) != 1 {
				return nil, forbidden
			}
		}
		ctx = context.WithValue(ctx, principalKey{}, p)
		ctx = app.WithAuthorization(ctx, s.checkCredential(p))
		return next(ctx, w, r, request)
	}
}

type loginLimit struct {
	count int
	until time.Time
}
type loginGuard struct {
	mu       sync.Mutex
	attempts map[string]loginLimit
	hashMu   sync.Mutex
}

func (s *Server) Login(ctx context.Context, r api.LoginRequestObject) (api.LoginResponseObject, error) {
	if r.Body == nil || len(r.Body.Password) == 0 || len(r.Body.Password) > 1024 {
		return nil, app.ErrInvalid
	}
	x := ctx.Value(requestKey{}).(exchange)
	ip, _, _ := net.SplitHostPort(x.r.RemoteAddr)
	now := s.App.Now()
	s.login.mu.Lock()
	if s.login.attempts == nil {
		s.login.attempts = map[string]loginLimit{}
	}
	for k, v := range s.login.attempts {
		if !now.Before(v.until) {
			delete(s.login.attempts, k)
		}
	}
	lim := s.login.attempts[ip]
	if len(s.login.attempts) >= 1024 || lim.count >= 5 {
		s.login.mu.Unlock()
		return nil, authError{429, "login_rate_limited"}
	}
	if lim.count == 0 {
		lim.until = now.Add(15 * time.Minute)
	}
	lim.count++
	s.login.attempts[ip] = lim
	s.login.mu.Unlock()
	s.login.hashMu.Lock()
	var encoded string
	e := s.App.Store.Read.QueryRowContext(ctx, `SELECT password_hash FROM account WHERE id=1`).Scan(&encoded)
	ok := e == nil && verifyPassword(encoded, r.Body.Password)
	s.login.hashMu.Unlock()
	if !ok {
		return nil, unauthorized
	}
	token, csrf := random(), random()
	_, e = s.App.Store.Write.ExecContext(ctx, `INSERT INTO sessions(hash,csrf,expires_at,created_at)VALUES(?,?,?,?)`, digest(token), csrf, date(now.Add(14*24*time.Hour)), date(now))
	if e != nil {
		return nil, e
	}
	s.login.mu.Lock()
	delete(s.login.attempts, ip)
	s.login.mu.Unlock()
	http.SetCookie(x.w, &http.Cookie{Name: "shiliu_session", Value: token, Path: "/", HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: 14 * 86400})
	return api.Login200JSONResponse{Authenticated: true, CsrfToken: csrf}, nil
}
func (s *Server) GetSession(ctx context.Context, r api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	p := getPrincipal(ctx)
	return api.GetSession200JSONResponse{Authenticated: p.Owner, CsrfToken: p.CSRF}, nil
}
func (s *Server) Logout(ctx context.Context, r api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	p := getPrincipal(ctx)
	_, e := s.App.Store.Write.ExecContext(ctx, `DELETE FROM sessions WHERE hash=?`, p.Hash)
	if e != nil {
		return nil, e
	}
	x := ctx.Value(requestKey{}).(exchange)
	http.SetCookie(x.w, &http.Cookie{Name: "shiliu_session", Value: "", Path: "/", HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	return api.Logout204Response{}, nil
}
func (s *Server) ListTokens(ctx context.Context, r api.ListTokensRequestObject) (api.ListTokensResponseObject, error) {
	rows, e := s.App.Store.Read.QueryContext(ctx, `SELECT id,name,scopes,created_at,revoked_at FROM tokens ORDER BY created_at DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := api.ListTokens200JSONResponse{}
	for rows.Next() {
		var v api.Token
		var scopes, created string
		var revoked sql.NullString
		if e = rows.Scan(&v.Id, &v.Name, &scopes, &created, &revoked); e != nil {
			return nil, e
		}
		json.Unmarshal([]byte(scopes), &v.Scopes)
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if revoked.Valid {
			t, _ := time.Parse(time.RFC3339Nano, revoked.String)
			v.RevokedAt = &t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Server) CreateToken(ctx context.Context, r api.CreateTokenRequestObject) (api.CreateTokenResponseObject, error) {
	if r.Body == nil || strings.TrimSpace(r.Body.Name) == "" || len(r.Body.Name) > 200 || len(r.Body.Scopes) == 0 || len(r.Body.Scopes) > 4 {
		return nil, app.ErrInvalid
	}
	allowed := map[string]bool{"articles:read": true, "artifacts:write": true, "fulltext:request": true, "reading:write": true}
	for _, v := range r.Body.Scopes {
		if !allowed[v] {
			return nil, app.ErrInvalid
		}
	}
	value := random()
	v := api.Token{Id: random(), Name: r.Body.Name, Scopes: r.Body.Scopes, CreatedAt: s.App.Now()}
	encoded, _ := json.Marshal(v.Scopes)
	e := s.authorizedWrite(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO tokens(id,name,hash,scopes,created_at)VALUES(?,?,?,?,?)`, v.Id, v.Name, digest(value), string(encoded), date(v.CreatedAt))
		return err
	})
	if e != nil {
		return nil, e
	}
	return api.CreateToken201JSONResponse{Token: value, Record: v}, nil
}
func (s *Server) RevokeToken(ctx context.Context, r api.RevokeTokenRequestObject) (api.RevokeTokenResponseObject, error) {
	e := s.authorizedWrite(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at=COALESCE(revoked_at,?) WHERE id=?`, date(s.App.Now()), r.Id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return app.ErrNotFound
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return api.RevokeToken204Response{}, nil
}
func writeProblem(w http.ResponseWriter, r *http.Request, e error) {
	status, code := 500, "internal_error"
	var auth authError
	switch {
	case errors.As(e, &auth):
		status, code = auth.status, auth.code
	case errors.Is(e, app.ErrNotFound), errors.Is(e, sql.ErrNoRows):
		status, code = 404, "not_found"
	case errors.Is(e, app.ErrAgentDisabled):
		status, code = 409, "agent_disabled"
	case errors.Is(e, app.ErrAgentBudget):
		status, code = 429, "agent_budget_exhausted"
	case errors.Is(e, app.ErrInvalid):
		status, code = 400, "invalid_request"
	case errors.Is(e, app.ErrConflict):
		status, code = 409, "conflict"
	case errors.Is(e, app.ErrVersion):
		status, code = 412, "version_mismatch"
	case errors.Is(e, context.DeadlineExceeded):
		status, code = 503, "request_timeout"
	}
	w.Header().Set("Content-Type", "application/problem+json")
	if status == 429 {
		w.Header().Set("Retry-After", "900")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Problem{Type: "urn:shiliu:error:" + code, Title: http.StatusText(status), Status: int64(status), Code: code, RequestId: w.Header().Get("X-Request-ID"), Retryable: status >= 500 || status == 429})
}
func convert[T any](v any) (T, error) {
	var out T
	b, e := json.Marshal(v)
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	if e != nil {
		return out, fmt.Errorf("response conversion: %w", e)
	}
	return out, nil
}

func (s *Server) authorizedWrite(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, e := s.App.Store.Write.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.checkCredential(getPrincipal(ctx))(ctx, tx); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
