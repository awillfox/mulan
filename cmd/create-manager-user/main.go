package main

import (
	"context"
	"flag"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/config"
	"mulan/internal/managerauth/service"
	"mulan/sqlc"
)

func main() {
	username := flag.String("username", "", "login username")
	password := flag.String("password", "", "password")
	name := flag.String("name", "", "display name")
	role := flag.String("role", "owner", "role: owner|staff")
	flag.Parse()

	if *username == "" || *password == "" || *name == "" {
		log.Fatal("usage: create-manager-user -username U -password P -name N [-role owner|staff]")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.PSQLURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	svc := service.NewService(sqlc.New(pool))
	u, err := svc.CreateUser(context.Background(), *username, *password, *name, *role)
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	log.Printf("created manager user id=%d username=%s role=%s", u.ID, u.Username, u.Role)
}
