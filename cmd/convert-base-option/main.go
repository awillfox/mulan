// Command convert-base-option migrates legacy delta-based "Serve" option
// groups into the absolute-priced menu_base_options model.
//
//	go run ./cmd/convert-base-option              # dry-run, prints the plan
//	go run ./cmd/convert-base-option --apply      # commit in a transaction
//	go run ./cmd/convert-base-option --source-name Size
//
// Runs against PSQL_URL (the local DB). Never point --db or PSQL_URL at
// production; production is migrated at deploy time.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/config"
	"mulan/sqlc"
)

func main() {
	apply := flag.Bool("apply", false, "commit changes (default: dry-run)")
	sourceName := flag.String("source-name", "Serve", "option group name to convert")
	dbURL := flag.String("db", "", "override PSQL_URL (local DB only)")
	flag.Parse()

	dsn := *dbURL
	if dsn == "" {
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		dsn = cfg.PSQLURL
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()
	q := sqlc.New(pool)

	links, err := q.ListMenuLinksByGroupName(ctx, *sourceName)
	if err != nil {
		log.Fatalf("list links: %v", err)
	}
	if len(links) == 0 {
		fmt.Printf("No menus linked to a group named %q. Nothing to do.\n", *sourceName)
		return
	}

	var converted, skipped, clonesDeleted int
	for _, l := range links {
		existing, err := q.ListMenuBaseOptions(ctx, l.MenuID)
		if err != nil {
			log.Fatalf("check existing base options (menu %d): %v", l.MenuID, err)
		}
		if len(existing) > 0 {
			fmt.Printf("menu %d: SKIP — already has %d base option(s)\n", l.MenuID, len(existing))
			skipped++
			continue
		}
		opts, err := q.ListOptionsByGroup(ctx, l.GroupID)
		if err != nil {
			log.Fatalf("list options (group %d): %v", l.GroupID, err)
		}
		isolated := l.OwnerMenuID.Valid && l.OwnerMenuID.Int32 == l.MenuID

		fmt.Printf("menu %d (price %.2f฿) via group %d %q:\n", l.MenuID, float64(l.MenuPrice)/100, l.GroupID, *sourceName)
		for _, o := range opts {
			base := basePriceFor(l.MenuPrice, o.PriceDelta)
			fmt.Printf("    %-16s delta %+d -> base %.2f฿\n", o.Name, o.PriceDelta, float64(base)/100)
		}
		if isolated {
			fmt.Println("    actions: detach link + delete isolated clone")
		} else {
			fmt.Println("    actions: detach link (shared preset kept)")
		}

		if *apply {
			if err := applyOne(ctx, pool, q, l, opts, isolated); err != nil {
				log.Fatalf("apply menu %d: %v", l.MenuID, err)
			}
			if isolated {
				clonesDeleted++
			}
		}
		converted++
	}

	mode := "DRY-RUN (no writes)"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Printf("\n%s — %d converted, %d skipped, %d isolated clones deleted\n", mode, converted, skipped, clonesDeleted)
	if !*apply && converted > 0 {
		fmt.Println("Re-run with --apply to commit (against the local DB only).")
	}
}

// applyOne converts a single (menu, group) link inside one transaction:
// insert base options, detach the menu link, and delete the group only when
// it is an isolated per-menu clone (shared presets are left intact). The pool
// is needed for BeginTx; q.WithTx binds the queries to the transaction.
func applyOne(ctx context.Context, pool *pgxpool.Pool, q *sqlc.Queries, l sqlc.ListMenuLinksByGroupNameRow, opts []sqlc.Option, isolated bool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	for i, o := range opts {
		if _, err := qtx.CreateMenuBaseOption(ctx, sqlc.CreateMenuBaseOptionParams{
			MenuID:    l.MenuID,
			Name:      o.Name,
			Price:     basePriceFor(l.MenuPrice, o.PriceDelta),
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("create base option: %w", err)
		}
	}
	if err := qtx.DetachMenuOptionGroup(ctx, sqlc.DetachMenuOptionGroupParams{
		MenuID:        l.MenuID,
		OptionGroupID: l.GroupID,
	}); err != nil {
		return fmt.Errorf("detach: %w", err)
	}
	if isolated {
		if err := qtx.DeleteOptionGroup(ctx, l.GroupID); err != nil {
			return fmt.Errorf("delete isolated group: %w", err)
		}
	}
	return tx.Commit(ctx)
}
