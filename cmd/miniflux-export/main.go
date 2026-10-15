// miniflux-export is a local read-only snapshot exporter. DSNs are supplied via
// the environment so credentials do not appear in process arguments or logs.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	_ "github.com/jackc/pgx/v5/stdlib"
	"shiliu-rss/internal/importer"
)

func main() {
	if err := run(); err != nil {
		var schemaError *importer.SchemaError
		if errors.As(err, &schemaError) {
			fmt.Fprintln(os.Stderr, schemaError.Error())
		} else {
			fmt.Fprintln(os.Stderr, "export failed; check required flags, supported source version and protected local database connection")
		}
		fmt.Fprintln(os.Stderr, "no complete snapshot was produced; discard partial output")
		os.Exit(1)
	}
}
func run() error {
	instance := flag.String("instance", "", "stable source instance label (no URL or credentials)")
	version := flag.String("source-version", "2.3.0", "operator-verified Miniflux release")
	user := flag.Int64("user-id", 0, "Miniflux user ID to export")
	flag.Parse()
	dsn := os.Getenv("MINIFLUX_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("MINIFLUX_DATABASE_URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return importer.ExportPostgres(ctx, db, os.Stdout, importer.ExportOptions{SourceInstance: *instance, SourceVersion: *version, UserID: *user})
}
