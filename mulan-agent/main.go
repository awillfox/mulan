package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/spf13/viper"

	"mulan-agent/lib/cashdrawer"
	"mulan-agent/lib/vfd"
)

//go:embed templates
var templateFS embed.FS

type vfdItem struct {
	Name  string  `json:"name"`
	Qty   int     `json:"qty"`
	Total float64 `json:"total"`
}

type vfdController struct {
	engine *vfd.Engine
	itemCh chan vfdItem
}

func newVFDController(engine *vfd.Engine) *vfdController {
	return &vfdController{
		engine: engine,
		itemCh: make(chan vfdItem, 1),
	}
}

func (c *vfdController) send(item vfdItem) {
	select {
	case c.itemCh <- item:
	default:
		// replace pending item with latest
		select {
		case <-c.itemCh:
		default:
		}
		c.itemCh <- item
	}
}

func (c *vfdController) run() {
	idle := []string{"Hua Mulan", "Mulan Project"}
	idleIdx := 0

	showIdle := func() {
		c.engine.Write(idle[idleIdx%len(idle)])
		idleIdx++
	}

	timer := time.NewTimer(3 * time.Second)
	for {
		select {
		case item := <-c.itemCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			line1 := fmt.Sprintf("%-15s x%3d", truncate(item.Name, 15), item.Qty)
			line2 := fmt.Sprintf("%-8s%12.2f", "Total", item.Total)
			c.engine.WriteLines(line1, line2)
			log.Printf("VFD item: %s | %s", line1, line2)
			timer.Reset(5 * time.Second)
		case <-timer.C:
			showIdle()
			timer.Reset(3 * time.Second)
		}
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func main() {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	viper.SetDefault("API_BASE", "http://localhost:8080")
	viper.SetDefault("PORT", "8081")
	viper.SetDefault("INPOUTX64_DLL", `C:\Tools\inpoutx64.dll`)
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("no .env file found, using defaults: %v", err)
	}

	apiBase := viper.GetString("API_BASE")
	port := viper.GetString("PORT")

	cashdrawer.Init(viper.GetString("INPOUTX64_DLL"))

	var ctrl *vfdController
	vfdEngine, err := vfd.New("COM3")
	if err != nil {
		log.Printf("VFD unavailable (COM3): %v", err)
	} else {
		defer vfdEngine.Close()
		vfdEngine.Clear()
		ctrl = newVFDController(vfdEngine)
		go ctrl.run()
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	r.Get("/pos", posHandler(apiBase))
	r.Post("/vfd/item", vfdItemHandler(ctrl))
	r.Post("/cash-drawer/open", cashDrawerHandler())

	log.Printf("mulan-agent starting on :%s (API_BASE=%s)", port, apiBase)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func vfdItemHandler(ctrl *vfdController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctrl == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var item vfdItem
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		ctrl.send(item)
		w.WriteHeader(http.StatusNoContent)
	}
}

func cashDrawerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := cashdrawer.Open(); err != nil {
			log.Printf("cash drawer error: %v", err)
			http.Error(w, "failed to open cash drawer", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func posHandler(apiBase string) http.HandlerFunc {
	tmpl := template.Must(template.ParseFS(templateFS,
		"templates/layouts/pos.html",
		"templates/pos/index.html",
	))

	return func(w http.ResponseWriter, r *http.Request) {
		data := map[string]string{
			"APIBase": apiBase,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "pos.html", data); err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
			log.Printf("template render error: %v", err)
		}
	}
}
