package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	baseoptionhttp "mulan/internal/baseoption/http"
	baseoptionservice "mulan/internal/baseoption/service"
	cashdrawerhttp "mulan/internal/cashdrawer/http"
	cashdrawerservice "mulan/internal/cashdrawer/service"
	cashierhttp "mulan/internal/cashier/http"
	cashierservice "mulan/internal/cashier/service"
	"mulan/internal/config"
	dashboardhttp "mulan/internal/dashboard/http"
	dashboardservice "mulan/internal/dashboard/service"
	discounthttp "mulan/internal/discount/http"
	discountservice "mulan/internal/discount/service"
	guestwifihttp "mulan/internal/guestwifi/http"
	guestwifiservice "mulan/internal/guestwifi/service"
	"mulan/internal/hub"
	managerauthdomain "mulan/internal/managerauth/domain"
	managerauthhttp "mulan/internal/managerauth/http"
	managerauthservice "mulan/internal/managerauth/service"
	memberhttp "mulan/internal/member/http"
	memberservice "mulan/internal/member/service"
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

	optionGroupSvc := optiongroupservice.NewService(pool, queries)
	optionGroupHandler := optiongrouphttp.NewHandler(optionGroupSvc)

	baseOptionSvc := baseoptionservice.NewService(pool, queries)
	baseOptionHandler := baseoptionhttp.NewHandler(baseOptionSvc)

	menuSvc := menuservice.NewMenuService(queries)
	menuHandler := menuhttp.NewMenuHandler(menuSvc, optionGroupSvc, baseOptionSvc, eventHub)

	categorySvc := menucategoryservice.NewCategoryService(queries)
	categoryHandler := menucategoryhttp.NewCategoryHandler(categorySvc)

	settingsSvc, err := settingsservice.NewSettingsService(ctx, queries)
	if err != nil {
		log.Fatalf("init settings: %v", err)
	}
	settingsHandler := settingshttp.NewHandler(settingsSvc)

	wifiSvc := guestwifiservice.New(pool, guestwifiservice.Config{
		Host:          cfg.MikrotikHost,
		Port:          cfg.MikrotikPort,
		User:          cfg.MikrotikUser,
		Password:      cfg.MikrotikPassword,
		HotspotServer: cfg.MikrotikHotspotServer,
	})
	if cfg.MikrotikHost != "" {
		if err := wifiSvc.FillPool(ctx); err != nil {
			log.Printf("guestwifi: initial pool fill: %v", err)
		}
		wifiSvc.ExpireLoop(ctx)
	}
	wifiHandler := guestwifihttp.New(wifiSvc)

	orderSvc := orderservice.NewOrderService(pool, queries, settingsSvc)
	var wifiDep orderhttp.WifiService
	if cfg.MikrotikHost != "" {
		wifiDep = wifiSvc
	}
	orderHandler := orderhttp.NewHandler(orderSvc, wifiDep)

	memberSvc := memberservice.NewService(queries)
	memberHandler := memberhttp.NewHandler(memberSvc)

	cashierSvc := cashierservice.NewService(queries)
	cashierHandler := cashierhttp.NewHandler(cashierSvc)

	dashboardSvc := dashboardservice.NewDashboardService(queries)
	dashboardHandler := dashboardhttp.NewHandler(dashboardSvc)

	cashDrawerSvc := cashdrawerservice.NewService(pool, queries)
	if err := cashDrawerSvc.SeedDenominations(ctx); err != nil {
		log.Fatalf("seed cash drawer denominations: %v", err)
	}
	cashDrawerHandler := cashdrawerhttp.NewHandler(cashDrawerSvc)

	discountSvc := discountservice.NewService(pool, queries)
	discountHandler := discounthttp.NewHandler(discountSvc, eventHub)

	managerAuthSvc := managerauthservice.NewService(queries)
	managerAuthHandler := managerauthhttp.NewHandler(managerAuthSvc)

	webHandler := web.NewHandler("templates")

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	r.Get("/manager", webHandler.Manager)
	r.Get("/manager/items", webHandler.Items)
	r.Get("/manager/option-groups", webHandler.OptionGroups)
	r.Get("/manager/discounts", webHandler.Discounts)
	r.Get("/manager/members", webHandler.Members)
	r.Get("/manager/cashiers", webHandler.Cashiers)
	r.Get("/manager/settings", webHandler.Settings)
	r.Get("/events", eventHub.ServeHTTP)
	// Logo lives in the settings table so it survives redeploys and is shared
	// across terminals. We intercept the specific path before falling back
	// to the static file server for everything else under /elements.
	r.Get("/elements/logo.png", settingsHandler.ServeLogo)
	r.Handle("/elements/*", http.StripPrefix("/elements/", http.FileServer(http.Dir("elements"))))

	r.Route("/api", func(r chi.Router) {
		// ---------- OPEN: POS / agent / shared (no auth) ----------
		r.Get("/menus", menuHandler.List)
		r.Get("/menu-categories", categoryHandler.List)
		r.Get("/settings", settingsHandler.Get)
		r.Get("/settings/logo", settingsHandler.GetLogo)
		r.Get("/members/lookup", memberHandler.Lookup)
		r.Post("/cashiers/login", cashierHandler.Login)
		r.Route("/orders", orderHandler.Routes)
		r.Route("/cash-drawer", cashDrawerHandler.Routes)
		r.Mount("/wifi", wifiHandler.Routes())
		r.Get("/discounts/active", discountHandler.ListActive)
		r.Route("/auth", managerAuthHandler.Routes) // POST /auth/login

		// ---------- RequireManager: any logged-in manager (reads) ----------
		r.Group(func(r chi.Router) {
			r.Use(managerauthhttp.RequireManager(managerAuthSvc))

			r.Post("/auth/logout", managerAuthHandler.Logout)
			r.Get("/auth/me", managerAuthHandler.Me)
			r.Post("/auth/change-password", managerAuthHandler.ChangePassword)
			r.Get("/discounts", discountHandler.List)
			r.Get("/option-groups", optionGroupHandler.ListGroups)
			r.Get("/members", memberHandler.List)
			r.Get("/members/{id}/orders", memberHandler.Orders)
			r.Get("/cashiers", cashierHandler.List)

			// ---------- RequireRole(owner): writes + owner data ----------
			r.Group(func(r chi.Router) {
				r.Use(managerauthhttp.RequireRole(managerauthdomain.RoleOwner))

				r.Post("/discounts", discountHandler.Create)
				r.Patch("/discounts/{id}", discountHandler.Update)
				r.Delete("/discounts/{id}", discountHandler.Delete)
				r.Route("/dashboard", dashboardHandler.Routes)
				r.Post("/menus", menuHandler.Create)
				r.Patch("/menus/{id}", menuHandler.Update)
				r.Patch("/menus/{id}/toggle", menuHandler.Toggle)
				r.Delete("/menus/{id}", menuHandler.Delete)
				r.Put("/menus/{id}/option-groups", optionGroupHandler.SetMenuGroups)
				r.Put("/menus/{id}/base-options", baseOptionHandler.SetMenuBaseOptions)
				r.Post("/menu-categories", categoryHandler.Create)
				r.Patch("/menu-categories/{id}", categoryHandler.Update)
				r.Delete("/menu-categories/{id}", categoryHandler.Delete)
				r.Post("/option-groups", optionGroupHandler.CreateGroup)
				r.Patch("/option-groups/{id}", optionGroupHandler.UpdateGroup)
				r.Delete("/option-groups/{id}", optionGroupHandler.DeleteGroup)
				r.Post("/option-groups/{id}/options", optionGroupHandler.CreateOption)
				r.Patch("/options/{id}", optionGroupHandler.UpdateOption)
				r.Delete("/options/{id}", optionGroupHandler.DeleteOption)
				r.Post("/members", memberHandler.Create)
				r.Patch("/members/{id}", memberHandler.Update)
				r.Delete("/members/{id}", memberHandler.Delete)
				r.Post("/cashiers", cashierHandler.Create)
				r.Patch("/cashiers/{id}", cashierHandler.Update)
				r.Patch("/cashiers/{id}/pin", cashierHandler.UpdatePin)
				r.Delete("/cashiers/{id}", cashierHandler.Delete)
				r.Patch("/settings", settingsHandler.Update)
				r.Put("/settings/logo", settingsHandler.PutLogo)
				r.Delete("/settings/logo", settingsHandler.DeleteLogo)
			})
		})
	})

	log.Println("server starting on :" + cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
