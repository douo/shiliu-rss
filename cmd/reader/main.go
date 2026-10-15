package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gofrs/flock"
	"shiliu-rss/internal/agent"
	"shiliu-rss/internal/app"
	"shiliu-rss/internal/fetch"
	httpapi "shiliu-rss/internal/http"
	"shiliu-rss/internal/importer"
	"shiliu-rss/internal/jobs"
	"shiliu-rss/internal/store"
	webassets "shiliu-rss/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	if e := run(); e != nil {
		slog.Error("command_failed", "reason", safeError(e))
		os.Exit(1)
	}
}
func safeError(e error) string {
	switch e {
	case app.ErrInvalid:
		return "invalid_input"
	case app.ErrConflict:
		return "conflict"
	default:
		return "operation_failed (check command arguments, database availability and configuration)"
	}
}
func run() error {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if cmd == "version" {
		fmt.Println("shiliu-rss 0.1.0")
		return nil
	}
	if cmd == "healthcheck" {
		addr := env("SHILIU_ADDR", "127.0.0.1:41001")
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		resp, e := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/readyz")
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return app.ErrConflict
		}
		return nil
	}
	if cmd == "preview" {
		if len(args) != 1 {
			return app.ErrInvalid
		}
		f, e := os.Open(args[0])
		if e != nil {
			return e
		}
		defer f.Close()
		report, e := importer.Preview(f)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	if cmd == "restore" {
		if len(args) != 2 {
			return app.ErrInvalid
		}
		if e := os.MkdirAll(filepath.Dir(args[1]), 0700); e != nil {
			return e
		}
		restoreLock := flock.New(args[1] + ".lock")
		locked, e := restoreLock.TryLock()
		if e != nil {
			return e
		}
		if !locked {
			return app.ErrConflict
		}
		defer restoreLock.Unlock()
		return importer.Restore(ctx, args[0], args[1], time.Now())
	}
	path := env("SHILIU_DB", ".local/reader.db")
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	lock := flock.New(path + ".lock")
	locked, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !locked {
		return app.ErrConflict
	}
	defer lock.Unlock()
	db, e := store.Open(ctx, path)
	if e != nil {
		return e
	}
	defer db.Close()
	service := app.New(db)
	switch cmd {
	case "init":
		passwordPath := os.Getenv("SHILIU_PASSWORD_FILE")
		if passwordPath == "" {
			return app.ErrInvalid
		}
		b, e := os.ReadFile(passwordPath)
		if e != nil {
			return e
		}
		if e = httpapi.Initialize(ctx, service, strings.TrimSpace(string(b))); e != nil {
			return e
		}
		fmt.Println("Account initialized. Use the password from SHILIU_PASSWORD_FILE to sign in.")
		return nil
	case "import":
		if len(args) < 1 || len(args) > 2 {
			return app.ErrInvalid
		}
		allow := len(args) == 2 && args[1] == "--allow-unsupported"
		if len(args) == 2 && !allow {
			return app.ErrInvalid
		}
		f, e := os.Open(args[0])
		if e != nil {
			return e
		}
		defer f.Close()
		report, e := importer.Import(ctx, f, service, importer.ImportOptions{AllowUnsupported: allow})
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	case "backup":
		if len(args) != 1 {
			return app.ErrInvalid
		}
		p, e := importer.Backup(ctx, service, args[0], time.Now())
		if e == nil {
			fmt.Println(p)
		}
		return e
	case "cleanup":
		for {
			n, e := service.Cleanup(ctx, 100)
			if e != nil {
				return e
			}
			if n < 100 {
				break
			}
		}
		return nil
	case "migrate":
		return service.Integrity(ctx)
	case "serve":
		var count int
		if e := db.Read.QueryRowContext(ctx, "SELECT count(*) FROM account").Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			fmt.Fprintln(os.Stderr, "Run make init (or reader init with SHILIU_PASSWORD_FILE) first.")
			return app.ErrInvalid
		}
		for {
			n, e := service.Cleanup(ctx, 100)
			if e != nil {
				return e
			}
			if n < 100 {
				break
			}
		}
		runner := jobs.New(service, fetch.New())
		runner.BackupDir = os.Getenv("SHILIU_BACKUP_DIR")
		done := make(chan struct{})
		go func() { runner.Run(ctx); close(done) }()
		agentRunner := agent.New(service)
		agentRunner.FetchWake = runner.Wake
		agentDone := make(chan struct{})
		go func() { agentRunner.Run(ctx); close(agentDone) }()
		defer func() { cancel(); <-agentDone }()
		server := &httpapi.Server{App: service, Origin: env("SHILIU_ORIGIN", "http://127.0.0.1:41001"), CookieSecure: env("SHILIU_COOKIE_SECURE", "true") != "false", Scheduler: runner, Agent: agentRunner, Assets: webassets.Files()}
		httpServer := &http.Server{Addr: env("SHILIU_ADDR", "127.0.0.1:41001"), Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
		errch := make(chan error, 1)
		go func() { slog.Info("reader_started"); errch <- httpServer.ListenAndServe() }()
		select {
		case <-ctx.Done():
		case e = <-errch:
			if e != http.ErrServerClosed {
				cancel()
				<-done
				return e
			}
		}
		stop, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		e = httpServer.Shutdown(stop)
		cancel()
		<-done
		return e
	default:
		io.WriteString(os.Stderr, "Commands: init, serve, migrate, preview FILE, import FILE [--allow-unsupported], backup DIR, restore BACKUP NEW_DB, cleanup, healthcheck, version\n")
		return app.ErrInvalid
	}
}
