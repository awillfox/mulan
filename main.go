package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/viper"

	menuhttp "mulan/internal/menu/http"
	menuservice "mulan/internal/menu/service"
	menucategoryhttp "mulan/internal/menucategory/http"
	menucategoryservice "mulan/internal/menucategory/service"
	"mulan/internal/web"
)

func main() {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("failed to read .env: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, viper.GetString("PSQL_URL"))
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("connected to database")

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	menuService := menuservice.NewMenuService()
	menuHandler := menuhttp.NewMenuHandler(menuService)

	categoryService := menucategoryservice.NewCategoryService()
	categoryHandler := menucategoryhttp.NewCategoryHandler(categoryService)

	webHandler := web.NewHandler("templates")

	r.Get("/pos", webHandler.POS)
	r.Get("/manager", webHandler.Manager)

	r.Route("/api/menus", menuHandler.Routes)
	r.Route("/api/menu-categories", categoryHandler.Routes)

	log.Println("server starting on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
