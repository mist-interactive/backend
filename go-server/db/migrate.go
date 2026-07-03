package db

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// set up migrations
//
//go:embed migrations/*.sql
var migrationFS embed.FS
var Migrations = migrate.NewMigrations()

func RunMigrations(ctx context.Context, db *bun.DB) error {
	fsys, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	err = Migrations.Discover(fsys)
	if err != nil {
		return err
	}

	migrator := migrate.NewMigrator(db, Migrations)
	migrator.Lock(ctx)
	defer func() { //need a detached context for unlock, to ensure it can run if the passed-in context gets cancelled
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = migrator.Unlock(unlockCtx)
	}()

	if err = migrator.Init(ctx); err != nil {
		return err
	}

	var group *migrate.MigrationGroup
	group, err = migrator.Migrate(ctx)
	if err != nil {
		return err
	}
	if len(group.Migrations) == 0 {
		log.Println("No migrations applied.")
		return nil
	}
	log.Printf("Applied migration set #%d:\n", group.ID)
	for _, m := range group.Migrations {
		log.Printf("  └─ ✓ Applied: % s\n", m.Name)
	}
	return nil
}
