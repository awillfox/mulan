package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/config"
	dashboardhttp "mulan/internal/dashboard/http"
	dashboardservice "mulan/internal/dashboard/service"
	"mulan/internal/hub"
	menuhttp "mulan/internal/menu/http"
	menuservice "mulan/internal/menu/service"
	menucategoryhttp "mulan/internal/menucategory/http"
	menucategoryservice "mulan/internal/menucategory/service"
	optiongrouphttp "mulan/internal/optiongroup/http"
	optiongroupservice "mulan/internal/optiongroup/service"
	orderhttp "mulan/internal/order/http"
	orderservice "mulan/internal/order/service"
	settingshttp "mulan/internal/settings/http"
	settingsservice "mulan/internal/settings/service"
	"mulan/internal/web"
	"mulan/sqlc"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.PSQLURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}
	log.Println("connected to database")

	queries := sqlc.New(pool)
	eventHub := hub.New()

	optionGroupSvc := optiongroupservice.NewService(queries)
	optionGroupHandler := optiongrouphttp.NewHandler(optionGroupSvc)

	menuSvc := menuservice.NewMenuService(queries)
	menuHandler := menuhttp.NewMenuHandler(menuSvc, optionGroupSvc, eventHub)

	categorySvc := menucategoryservice.NewCategoryService(queries)
	categoryHandler := menucategoryhttp.NewCategoryHandler(categorySvc)

	settingsSvc, err := settingsservice.NewSettingsService(ctx, queries)
	if err != nil {
		log.Fatalf("init settings: %v", err)
	}
	settingsHandler := settingshttp.NewHandler(settingsSvc)

	orderSvc := orderservice.NewOrderService(queries, settingsSvc, optionGroupSvc)
	orderHandler := orderhttp.NewHandler(orderSvc)

	dashboardSvc := dashboardservice.NewDashboardService(queries)
	dashboardHandler := dashboardhttp.NewHandler(dashboardSvc)

	webHandler := web.NewHandler("templates")

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	r.Get("/manager", webHandler.Manager)
	r.Get("/manager/items", webHandler.Items)
	r.Get("/manager/option-groups", webHandler.OptionGroups)
	r.Get("/manager/settings", webHandler.Settings)
	r.Get("/events", eventHub.ServeHTTP)
	r.Handle("/elements/*", http.StripPrefix("/elements/", http.FileServer(http.Dir("elements"))))

	r.Route("/api", func(r chi.Router) {
		r.Route("/menus", func(r chi.Router) {
			menuHandler.Routes(r)
			r.Put("/{id}/option-groups", optionGroupHandler.SetMenuGroups)
		})
		r.Route("/menu-categories", categoryHandler.Routes)
		r.Route("/option-groups", optionGroupHandler.Routes)
		r.Route("/options", optionGroupHandler.OptionRoutes)
		r.Route("/orders", orderHandler.Routes)
		r.Route("/settings", settingsHandler.Routes)
		r.Route("/dashboard", dashboardHandler.Routes)
	})

	log.Println("server starting on :" + cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
