package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/viper"

	"mulan/internal/hub"
	menuhttp "mulan/internal/menu/http"
	menuservice "mulan/internal/menu/service"
	menucategoryhttp "mulan/internal/menucategory/http"
	menucategoryservice "mulan/internal/menucategory/service"
	orderhttp "mulan/internal/order/http"
	orderservice "mulan/internal/order/service"
	"mulan/internal/web"
	"mulan/sqlc"
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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	queries := sqlc.New(pool)

	eventHub := hub.New()

	menuService := menuservice.NewMenuService(queries)
	menuHandler := menuhttp.NewMenuHandler(menuService, eventHub)

	categoryService := menucategoryservice.NewCategoryService(queries)
	categoryHandler := menucategoryhttp.NewCategoryHandler(categoryService)

	orderSvc := orderservice.NewOrderService(queries)
	orderHandler := orderhttp.NewHandler(orderSvc)

	webHandler := web.NewHandler("templates")

	r.Get("/manager", webHandler.Manager)
	r.Get("/manager/items", webHandler.Items)
	r.Get("/events", eventHub.ServeHTTP)

	r.Handle("/elements/*", http.StripPrefix("/elements/", http.FileServer(http.Dir("elements"))))

	r.Route("/api/menus", menuHandler.Routes)
	r.Route("/api/menu-categories", categoryHandler.Routes)
	r.Route("/api/orders", orderHandler.Routes)
	r.Get("/api/dashboard", orderHandler.DashboardSummary)
	r.Get("/api/dashboard/top-menus", orderHandler.TopMenus)
	port:= viper.GetString("PORT")
	log.Println("server starting on :"+port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
