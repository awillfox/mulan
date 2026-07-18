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
	"os"
	"strings"
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
	engine  *vfd.Engine
	itemCh  chan vfdItem
	linesCh chan [2]string
}

func newVFDController(engine *vfd.Engine) *vfdController {
	return &vfdController{
		engine:  engine,
		itemCh:  make(chan vfdItem, 1),
		linesCh: make(chan [2]string, 1),
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

// sendLines pushes a pre-formatted 2-line message (used for the payment
// display). Same latest-wins coalescing as send.
func (c *vfdController) sendLines(line1, line2 string) {
	msg := [2]string{line1, line2}
	select {
	case c.linesCh <- msg:
	default:
		select {
		case <-c.linesCh:
		default:
		}
		c.linesCh <- msg
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
		case msg := <-c.linesCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			c.engine.WriteLines(msg[0], msg[1])
			log.Printf("VFD lines: %s | %s", msg[0], msg[1])
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
	// Server (mulan) LAN address. Override per-device via .env if it moves.
	viper.SetDefault("API_BASE", "http://localhost:8085")
	viper.SetDefault("PORT", "8081")
	viper.SetDefault("INPOUTX64_DLL", `C:\Tools\inpoutx64.dll`)
	viper.SetDefault("RECEIPT_PRINTER_ADDR", "")
	// ESC/POS code-page index sent on every print so Thai (and other non-ASCII)
	// glyphs compose correctly. 26 = TIS-620 / Thai Character Code 18 (common
	// Epson TM-T default). Override for other printer families.
	viper.SetDefault("RECEIPT_PRINTER_CODEPAGE", 26)
	// POS-local payment-channel config (which of cash/card/QR are offered +
	// the default). Stored on disk beside the agent so it survives restarts.
	viper.SetDefault("POS_CONFIG_FILE", "pos-config.json")
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("no .env file found, using defaults: %v", err)
	}

	apiBase := viper.GetString("API_BASE")
	port := viper.GetString("PORT")
	payConfigStore := newPaymentConfigStore(viper.GetString("POS_CONFIG_FILE"))

	cashdrawer.Init(viper.GetString("INPOUTX64_DLL"))

	var rcptPrinter *printer.Printer
	if addr := viper.GetString("RECEIPT_PRINTER_ADDR"); addr != "" {
		logoBytes := fetchLogo(apiBase + "/elements/logo.png")
		codepage := byte(viper.GetInt("RECEIPT_PRINTER_CODEPAGE"))
		p, err := printer.New(addr, logoBytes, codepage)
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
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	r.Get("/pos", posHandler(apiBase))
	r.Post("/vfd/item", vfdItemHandler(ctrl))
	r.Post("/vfd/payment", vfdPaymentHandler(ctrl))
	r.Post("/cash-drawer/open", cashDrawerHandler())
	r.Post("/checkout", checkoutHandler(rcptPrinter, apiBase, payConfigStore))
	r.Get("/config/payment", getPaymentConfigHandler(payConfigStore))
	r.Put("/config/payment", putPaymentConfigHandler(payConfigStore))
	r.Post("/restart", restartHandler())

	log.Printf("mulan-agent starting on :%s (API_BASE=%s)", port, apiBase)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// restartHandler exits the agent process so NSSM (Windows) or systemd
// (Linux) respawns it. The agent runs under a supervisor in production —
// in dev the process just exits.
//
// We respond 202 before exiting so the cashier's browser sees the ack;
// the actual exit fires from a goroutine after a 200ms beat to flush the
// TCP buffer.
func restartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"restarting":true}`))
		log.Println("restart requested via /restart — exiting in 200ms")
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
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

// vfdPayment is the customer-display payload sent while the cashier is in
// the tender modal. The agent formats it into two 20-char VFD lines.
type vfdPayment struct {
	Method   string  `json:"method"` // "cash" | "card" | "qr"
	Total    float64 `json:"total"`
	Tendered float64 `json:"tendered"`
	Change   float64 `json:"change"`
}

// vfdCenter centres s within a 20-char VFD line.
func vfdCenter(s string) string {
	const w = 20
	if len(s) >= w {
		return s[:w]
	}
	pad := (w - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}

func vfdPaymentHandler(ctrl *vfdController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctrl == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var p vfdPayment
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		// Line 1 always shows the amount due.
		line1 := fmt.Sprintf("%-8s%12.2f", "TOTAL", p.Total)

		// Line 2 depends on payment method.
		var line2 string
		switch p.Method {
		case "card":
			line2 = vfdCenter("CARD PAYMENT")
		case "qr":
			line2 = vfdCenter("QR PROMPTPAY")
		default: // cash
			switch {
			case p.Tendered >= p.Total && p.Tendered > 0:
				line2 = fmt.Sprintf("%-8s%12.2f", "CHANGE", p.Change)
			case p.Tendered > 0:
				line2 = fmt.Sprintf("%-8s%12.2f", "CASH", p.Tendered)
			default:
				line2 = vfdCenter("CASH PAYMENT")
			}
		}

		ctrl.sendLines(line1, line2)
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

type checkoutRequestItem struct {
	MenuID       int32   `json:"menu_id"`
	Name         string  `json:"name"`
	Price        float64 `json:"price"`
	Qty          int32   `json:"qty"`
	BaseOptionID int32   `json:"base_option_id"`
	OptionIDs    []int32 `json:"option_ids"`
	DiscountIDs  []int32 `json:"discount_ids"`
}

type checkoutRequest struct {
	OrderCode     string                `json:"order_code"`
	Items         []checkoutRequestItem `json:"items"`
	DiscountIDs   []int32               `json:"discount_ids,omitempty"`   // whole-order discounts
	PaymentMethod string                `json:"payment_method,omitempty"` // "cash" | "card" | "qr"
	CashTendered  float64               `json:"cash_tendered,omitempty"`  // THB, cash payments only
	CashChange    float64               `json:"cash_change,omitempty"`    // THB, cash payments only
	CashTender    map[string]int        `json:"cash_tender,omitempty"`    // satang string -> count
	CashierName   string                `json:"cashier_name,omitempty"`
	CustomerPhone string                `json:"customer_phone,omitempty"`
	CustomerName  string                `json:"customer_name,omitempty"`
}

type checkoutOption struct {
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
}

type checkoutItem struct {
	Name           string           `json:"name"`
	Price          float64          `json:"price"`
	Qty            int32            `json:"qty"`
	Options        []checkoutOption `json:"options"`
	BaseOptionName string           `json:"base_option_name"`
}

type checkoutResponse struct {
	Code            string         `json:"code"`
	Subtotal        float64        `json:"subtotal"`
	Discount        float64        `json:"discount"`
	Subsidy         float64        `json:"subsidy"`
	VAT             float64        `json:"vat"`
	VATPercent      float64        `json:"vat_percent"`
	ShopName        string         `json:"shop_name"`
	ReceiptFooter   string         `json:"receipt_footer"`
	Total           float64        `json:"total"`
	Items           []checkoutItem `json:"items"`
	HasMember       bool           `json:"has_member"`
	MemberName      string         `json:"member_name"`
	MemberPhone     string         `json:"member_phone"`
	PointsEarned    int64          `json:"points_earned"`
	PointsBalance   int64          `json:"points_balance"`
	WifiUsername    string         `json:"wifi_username,omitempty"`
	RoundedDue      float64        `json:"rounded_due"`
	Change          float64        `json:"change"`
	ChangeBreakdown map[string]int `json:"change_breakdown"`
}

type checkoutEnvelope struct {
	Data  checkoutResponse `json:"data"`
	Error string           `json:"error,omitempty"`
}

func checkoutHandler(p *printer.Printer, apiBase string, payConfig *paymentConfigStore) http.HandlerFunc {
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
		result, upstreamStatus, err := callCheckout(apiBase, req.OrderCode, req.Items, req.DiscountIDs, req.CustomerPhone, req.CustomerName, req.PaymentMethod, req.CashTender, req.CashierName)
		if err != nil {
			log.Printf("checkout API error (upstream %d): %v", upstreamStatus, err)
			if upstreamStatus == http.StatusConflict {
				http.Error(w, err.Error(), http.StatusConflict)
			} else {
				http.Error(w, "checkout failed", http.StatusBadGateway)
			}
			return
		}

		// Print order bill (kitchen ticket) then receipt using backend-computed totals
		if p != nil {
			items := make([]printer.OrderItem, len(result.Items))
			for i, it := range result.Items {
				opts := make([]printer.OrderItemOption, len(it.Options))
				for j, o := range it.Options {
					opts[j] = printer.OrderItemOption{Name: o.Name, PriceDelta: o.PriceDelta}
				}
				items[i] = printer.OrderItem{
					Name:           it.Name,
					Qty:            int(it.Qty),
					Price:          it.Price,
					Options:        opts,
					BaseOptionName: it.BaseOptionName,
				}
			}
			if err := p.PrintOrderBill(req.OrderCode, items); err != nil {
				log.Printf("order bill print error: %v", err)
			}
			pay := printer.Payment{
				Method:   req.PaymentMethod,
				Tendered: req.CashTendered,
				Change:   req.CashChange,
			}
			member := printer.MemberInfo{
				Present: result.HasMember,
				Name:    result.MemberName,
				Phone:   result.MemberPhone,
				Earned:  result.PointsEarned,
				Balance: result.PointsBalance,
			}
			// PromptPay id is read per checkout rather than cached at startup so
			// a change in POS Settings takes effect on the next sale, with no
			// agent restart. A read failure only costs the QR block, so the
			// receipt still prints.
			promptPayID := ""
			if payConfig != nil {
				if cfg, err := payConfig.load(); err != nil {
					log.Printf("payment config load failed, receipt QR skipped: %v", err)
				} else {
					promptPayID = cfg.PromptPayID
				}
			}
			if err := p.PrintReceipt(result.ShopName, result.ReceiptFooter, items, result.Subtotal, result.Discount, result.Subsidy, result.VAT, result.VATPercent, result.Total, pay, member, result.WifiUsername, promptPayID); err != nil {
				log.Printf("receipt print error: %v", err)
			}
		}

		// Only kick the till open when the order is paid in cash. Card/QR
		// payments don't touch the drawer. Empty payment_method is treated
		// as cash for backwards compat with older clients.
		if req.PaymentMethod == "" || req.PaymentMethod == "cash" {
			if err := cashdrawer.Open(); err != nil {
				log.Printf("cash drawer error: %v", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// callCheckout POSTs to the backend checkout endpoint and returns the decoded
// response, the upstream HTTP status code, and any error. The upstream status is
// returned even on error so the caller can propagate meaningful codes (e.g. 409
// "cannot make change") rather than always responding 502.
func callCheckout(apiBase, code string, items any, discountIDs []int32, customerPhone, customerName, paymentMethod string, cashTender map[string]int, cashierName string) (*checkoutResponse, int, error) {
	payload := map[string]any{
		"items":          items,
		"discount_ids":   discountIDs,
		"customer_phone": customerPhone,
		"customer_name":  customerName,
		"payment_method": paymentMethod,
		"cash_tender":    cashTender,
		"cashier_name":   cashierName,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.Post(apiBase+"/api/orders/"+code+"/checkout", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	// Always attempt to decode the envelope so we can extract the error message.
	var env checkoutEnvelope
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if resp.StatusCode != http.StatusOK {
		msg := env.Error
		if msg == "" {
			msg = fmt.Sprintf("API returned %d", resp.StatusCode)
		}
		return nil, resp.StatusCode, fmt.Errorf("%s", msg)
	}
	if env.Error != "" {
		return nil, resp.StatusCode, fmt.Errorf("API error: %s", env.Error)
	}
	return &env.Data, resp.StatusCode, nil
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
