package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/spf13/viper"

	"mulan-agent/lib/cashdrawer"
	"mulan-agent/lib/printer"
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
	const clearAfter = 10 * time.Second
	timer := time.NewTimer(clearAfter)
	cleared := true // nothing shown yet, no need to clear on first timeout

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
			cleared = false
			timer.Reset(clearAfter)
		case <-timer.C:
			if !cleared {
				c.engine.Clear()
				cleared = true
				log.Println("VFD cleared after inactivity")
			}
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
	viper.SetDefault("RECEIPT_PRINTER_ADDR", "")
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("no .env file found, using defaults: %v", err)
	}

	apiBase := viper.GetString("API_BASE")
	port := viper.GetString("PORT")

	cashdrawer.Init(viper.GetString("INPOUTX64_DLL"))

	var rcptPrinter *printer.Printer
	if addr := viper.GetString("RECEIPT_PRINTER_ADDR"); addr != "" {
		logoBytes := fetchLogo(apiBase + "/elements/logo.png")
		p, err := printer.New(addr, logoBytes)
		if err != nil {
			log.Printf("receipt printer unavailable (%s): %v", addr, err)
		} else {
			rcptPrinter = p
		}
	} else {
		log.Println("receipt printer not configured (RECEIPT_PRINTER_ADDR not set)")
	}

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
	r.Post("/checkout", checkoutHandler(rcptPrinter, apiBase))

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

type checkoutRequest struct {
	OrderCode string `json:"order_code"`
	Items     []struct {
		MenuID int32   `json:"menu_id"`
		Name   string  `json:"name"`
		Price  float64 `json:"price"`
		Qty    int32   `json:"qty"`
	} `json:"items"`
}

type checkoutResponse struct {
	Code       string  `json:"code"`
	Subtotal   float64 `json:"subtotal"`
	VAT        float64 `json:"vat"`
	VATPercent float64 `json:"vat_percent"`
	ShopName   string  `json:"shop_name"`
	Total      float64 `json:"total"`
}

func checkoutHandler(p *printer.Printer, apiBase string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req checkoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if len(req.Items) == 0 || req.OrderCode == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Forward to mulan API to persist order items and get computed totals
		result, err := callCheckout(apiBase, req.OrderCode, req.Items)
		if err != nil {
			log.Printf("checkout API error: %v", err)
			http.Error(w, "checkout failed", http.StatusBadGateway)
			return
		}

		// Print receipt using backend-computed totals
		if p != nil {
			items := make([]printer.OrderItem, len(req.Items))
			for i, it := range req.Items {
				items[i] = printer.OrderItem{Name: it.Name, Qty: int(it.Qty), Price: it.Price}
			}
			if err := p.PrintReceipt(result.ShopName, items, result.Subtotal, result.VAT, result.VATPercent, result.Total); err != nil {
				log.Printf("receipt print error: %v", err)
			}
		}

		if err := cashdrawer.Open(); err != nil {
			log.Printf("cash drawer error: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func callCheckout(apiBase, code string, items any) (*checkoutResponse, error) {
	body, _ := json.Marshal(map[string]any{"items": items})
	resp, err := http.Post(apiBase+"/api/orders/"+code+"/checkout", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}
	var result checkoutResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func fetchLogo(url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("logo fetch failed (%s): %v", url, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("logo fetch failed (%s): status %d", url, resp.StatusCode)
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("logo read failed: %v", err)
		return nil
	}
	log.Printf("logo downloaded: %d bytes from %s", len(b), url)
	return b
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
