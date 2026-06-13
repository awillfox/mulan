package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// One-off backfill: set menus.vfd_name from menus.name (truncated to 20
// runes) for any row where vfd_name is NULL or empty.
// Usage: go run ./scripts/vfdfill          (dry run — list only)
//
//	go run ./scripts/vfdfill --apply  (write changes)
func main() {
	apply := len(os.Args) > 1 && os.Args[1] == "--apply"

	dsn := os.Getenv("PSQL_URL")
	if dsn == "" {
		log.Fatal("PSQL_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx,
		`SELECT id, name FROM menus WHERE vfd_name IS NULL OR vfd_name = '' ORDER BY id`)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	type menu struct {
		id   int32
		name string
	}
	var missing []menu
	for rows.Next() {
		var m menu
		if err := rows.Scan(&m.id, &m.name); err != nil {
			log.Fatalf("scan: %v", err)
		}
		missing = append(missing, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatalf("rows: %v", err)
	}

	if len(missing) == 0 {
		fmt.Println("No menus missing vfd_name. Nothing to do.")
		return
	}

	fmt.Printf("%d menu(s) missing vfd_name:\n", len(missing))
	for _, m := range missing {
		vfd := truncRunes(m.name, 20)
		fmt.Printf("  id=%d  %q -> vfd_name %q\n", m.id, m.name, vfd)
	}

	if !apply {
		fmt.Println("\nDry run. Re-run with --apply to write these changes.")
		return
	}

	updated := 0
	for _, m := range missing {
		tag, err := pool.Exec(ctx,
			`UPDATE menus SET vfd_name = $2 WHERE id = $1`, m.id, truncRunes(m.name, 20))
		if err != nil {
			log.Fatalf("update id=%d: %v", m.id, err)
		}
		updated += int(tag.RowsAffected())
	}
	fmt.Printf("\nDone. %d row(s) updated.\n", updated)
}

// truncRunes returns the first n runes of s (varchar(20) counts characters,
// so Thai/multibyte names stay within the column limit).
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
