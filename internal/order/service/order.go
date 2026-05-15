package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/order/domain"
	settingsservice "mulan/internal/settings/service"
	"mulan/sqlc"
)

// HeldOrder is the service-level view of a held order returned to handlers.
type HeldOrder struct {
	Code      string
	CreatedAt pgtype.Timestamptz
	HeldAt    pgtype.Timestamptz
	HeldLabel *string
	Payload   []byte
}

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Sentinel errors translated to specific HTTP statuses by the handler.
var (
	ErrAlreadyPaid     = errors.New("order already paid")
	ErrNoItems         = errors.New("no items")
	ErrUnknownMenu     = errors.New("unknown menu")
	ErrMenuInactive    = errors.New("menu inactive")
	ErrInvalidOption   = errors.New("option not allowed for menu")
	ErrUnknownOption   = errors.New("unknown option")
	ErrMissingRequired = errors.New("missing required option")
	ErrOrderNotFound   = errors.New("order not found")
	ErrNotHeld         = errors.New("order is not held")
	ErrCannotHold      = errors.New("order cannot be held")
)

type OrderService struct {
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	settings *settingsservice.SettingsService
}

func NewOrderService(pool *pgxpool.Pool, q *sqlc.Queries, settings *settingsservice.SettingsService) *OrderService {
	return &OrderService{pool: pool, q: q, settings: settings}
}

func (s *OrderService) Create(ctx context.Context) (string, error) {
	code, err := generateCode()
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	row, err := s.q.CreateOrder(ctx, code)
	if err != nil {
		return "", fmt.Errorf("create order: %w", err)
	}
	return row.Code, nil
}

// CheckoutItemInput is what the handler accepts from the client. The server
// only trusts MenuID, Qty, and OptionIDs — Name and Price are looked up
// server-side from the menus/options tables.
type CheckoutItemInput struct {
	MenuID    int32
	Qty       int32
	OptionIDs []int32
}

// Checkout finalises an open order: it validates every line against the
// authoritative menu/option data, persists order_items + order_item_options,
// marks the order paid, and returns totals. The whole flow runs in a single
// transaction so any failure rolls back cleanly. Calling Checkout on an
// already-paid order returns ErrAlreadyPaid so the caller can map it to 409
// instead of double-charging.
func (s *OrderService) Checkout(ctx context.Context, code string, items []CheckoutItemInput) (*domain.CheckoutResult, error) {
	if len(items) == 0 {
		return nil, ErrNoItems
	}
	if err := validateInputs(items); err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	order, err := q.GetOrderByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	if order.Status == "paid" {
		return nil, ErrAlreadyPaid
	}

	menuByID, err := loadMenus(ctx, q, items)
	if err != nil {
		return nil, err
	}
	allowedGroupByMenu, err := loadAllowedGroups(ctx, q, menuByID)
	if err != nil {
		return nil, err
	}
	optByID, err := loadOptions(ctx, q, items)
	if err != nil {
		return nil, err
	}

	resultItems := make([]domain.CheckoutResultItem, 0, len(items))
	var subtotalSatang int64

	for _, in := range items {
		m, ok := menuByID[in.MenuID]
		if !ok {
			return nil, ErrUnknownMenu
		}
		if !m.Active {
			return nil, ErrMenuInactive
		}

		opts, deltaSum, err := resolveLineOptions(in, optByID, allowedGroupByMenu[m.ID])
		if err != nil {
			return nil, err
		}

		unitPrice := m.Price + deltaSum
		subtotalSatang += unitPrice * int64(in.Qty)

		itemID, err := q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID: order.ID,
			MenuID:  pgtype.Int4{Int32: m.ID, Valid: true},
			Name:    m.Name,
			Price:   m.Price,
			Qty:     in.Qty,
		})
		if err != nil {
			return nil, fmt.Errorf("insert order item: %w", err)
		}
		for _, o := range opts {
			if err := q.CreateOrderItemOption(ctx, sqlc.CreateOrderItemOptionParams{
				OrderItemID: itemID,
				OptionID:    pgtype.Int4{Int32: o.ID, Valid: true},
				Name:        o.Name,
				PriceDelta:  o.PriceDelta,
			}); err != nil {
				return nil, fmt.Errorf("insert order item option: %w", err)
			}
		}

		resultItems = append(resultItems, domain.CheckoutResultItem{
			Name:    m.Name,
			Price:   m.Price,
			Qty:     in.Qty,
			Options: opts,
		})
	}

	if err := q.PayOrder(ctx, code); err != nil {
		return nil, fmt.Errorf("pay order: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	cfg := s.settings.Get()
	vatSatang := subtotalSatang * int64(cfg.VatPercent*100) / 10000
	totalSatang := subtotalSatang + vatSatang

	return &domain.CheckoutResult{
		Code:          code,
		Subtotal:      float64(subtotalSatang) / 100,
		VAT:           float64(vatSatang) / 100,
		VATPercent:    cfg.VatPercent,
		ShopName:      cfg.ShopName,
		ReceiptFooter: cfg.ReceiptFooter,
		Total:         float64(totalSatang) / 100,
		Items:         resultItems,
	}, nil
}

// Hold parks an open order. Payload is opaque JSON owned by the POS client
// (line items, options, etc.) so the order can be restored exactly on resume.
// Held orders survive process restarts and are visible across terminals.
func (s *OrderService) Hold(ctx context.Context, code string, label *string, payload []byte) (HeldOrder, error) {
	order, err := s.q.GetOrderByCode(ctx, code)
	if err != nil {
		return HeldOrder{}, fmt.Errorf("%w: %v", ErrOrderNotFound, err)
	}
	if order.Status == "paid" {
		return HeldOrder{}, ErrAlreadyPaid
	}
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	var lbl pgtype.Text
	if label != nil && *label != "" {
		lbl = pgtype.Text{String: *label, Valid: true}
	}
	row, err := s.q.HoldOrder(ctx, sqlc.HoldOrderParams{
		Code:        code,
		HeldLabel:   lbl,
		HeldPayload: payload,
	})
	if err != nil {
		return HeldOrder{}, fmt.Errorf("hold order: %w", err)
	}
	out := HeldOrder{
		Code:      row.Code,
		CreatedAt: row.CreatedAt,
		HeldAt:    row.HeldAt,
		Payload:   row.HeldPayload,
	}
	if row.HeldLabel.Valid {
		v := row.HeldLabel.String
		out.HeldLabel = &v
	}
	return out, nil
}

// Resume flips a held order back to open and returns the payload captured at
// hold time. The DB-side payload is cleared atomically so resuming twice is
// a no-op.
func (s *OrderService) Resume(ctx context.Context, code string) ([]byte, error) {
	row, err := s.q.ResumeOrder(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotHeld
		}
		return nil, fmt.Errorf("resume order: %w", err)
	}
	return row.HeldPayload, nil
}

// DiscardHeld permanently removes a held order. Only held orders are touched;
// the WHERE clause guards against accidentally deleting an open/paid row.
func (s *OrderService) DiscardHeld(ctx context.Context, code string) error {
	if err := s.q.DiscardHeldOrder(ctx, code); err != nil {
		return fmt.Errorf("discard held: %w", err)
	}
	return nil
}

// ListHeld returns all currently held orders, newest hold first.
func (s *OrderService) ListHeld(ctx context.Context) ([]HeldOrder, error) {
	rows, err := s.q.ListHeldOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list held: %w", err)
	}
	out := make([]HeldOrder, len(rows))
	for i, r := range rows {
		out[i] = HeldOrder{
			Code:      r.Code,
			CreatedAt: r.CreatedAt,
			HeldAt:    r.HeldAt,
			Payload:   r.HeldPayload,
		}
		if r.HeldLabel.Valid {
			v := r.HeldLabel.String
			out[i].HeldLabel = &v
		}
	}
	return out, nil
}

func validateInputs(items []CheckoutItemInput) error {
	for _, in := range items {
		if in.MenuID <= 0 {
			return ErrUnknownMenu
		}
		if in.Qty <= 0 {
			return fmt.Errorf("qty must be > 0 for menu %d", in.MenuID)
		}
	}
	return nil
}

func loadMenus(ctx context.Context, q *sqlc.Queries, items []CheckoutItemInput) (map[int32]sqlc.Menu, error) {
	ids := uniqueMenuIDs(items)
	rows, err := q.GetMenusByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load menus: %w", err)
	}
	out := make(map[int32]sqlc.Menu, len(rows))
	for _, m := range rows {
		out[m.ID] = m
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownMenu, id)
		}
	}
	return out, nil
}

func loadAllowedGroups(ctx context.Context, q *sqlc.Queries, menus map[int32]sqlc.Menu) (map[int32]map[int32]struct{}, error) {
	menuIDs := make([]int32, 0, len(menus))
	for id := range menus {
		menuIDs = append(menuIDs, id)
	}
	links, err := q.ListMenuOptionGroupLinks(ctx, menuIDs)
	if err != nil {
		return nil, fmt.Errorf("list menu option group links: %w", err)
	}
	out := make(map[int32]map[int32]struct{}, len(menus))
	for _, l := range links {
		set, ok := out[l.MenuID]
		if !ok {
			set = make(map[int32]struct{})
			out[l.MenuID] = set
		}
		set[l.OptionGroupID] = struct{}{}
	}
	return out, nil
}

func loadOptions(ctx context.Context, q *sqlc.Queries, items []CheckoutItemInput) (map[int32]sqlc.Option, error) {
	ids := uniqueOptionIDs(items)
	if len(ids) == 0 {
		return map[int32]sqlc.Option{}, nil
	}
	rows, err := q.GetOptionsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load options: %w", err)
	}
	out := make(map[int32]sqlc.Option, len(rows))
	for _, o := range rows {
		out[o.ID] = o
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownOption, id)
		}
	}
	return out, nil
}

// resolveLineOptions returns the (server-trusted) options for one line and
// their summed price delta. Each option must belong to an option group that
// is attached to the menu being ordered, otherwise we reject the line —
// otherwise a malicious client could pick a cheap menu and attach the
// "extra-large" option from a totally unrelated menu.
func resolveLineOptions(in CheckoutItemInput, optByID map[int32]sqlc.Option, allowedGroups map[int32]struct{}) ([]domain.SelectedOption, int64, error) {
	opts := make([]domain.SelectedOption, 0, len(in.OptionIDs))
	var delta int64
	for _, oid := range in.OptionIDs {
		o, ok := optByID[oid]
		if !ok {
			return nil, 0, fmt.Errorf("%w: %d", ErrUnknownOption, oid)
		}
		if _, allowed := allowedGroups[o.OptionGroupID]; !allowed {
			return nil, 0, fmt.Errorf("%w: option %d (group %d) not attached to menu %d", ErrInvalidOption, oid, o.OptionGroupID, in.MenuID)
		}
		opts = append(opts, domain.SelectedOption{ID: o.ID, Name: o.Name, PriceDelta: o.PriceDelta})
		delta += o.PriceDelta
	}
	return opts, delta, nil
}

func uniqueMenuIDs(items []CheckoutItemInput) []int32 {
	seen := make(map[int32]struct{}, len(items))
	out := make([]int32, 0, len(items))
	for _, it := range items {
		if _, ok := seen[it.MenuID]; ok {
			continue
		}
		seen[it.MenuID] = struct{}{}
		out = append(out, it.MenuID)
	}
	return out
}

func uniqueOptionIDs(items []CheckoutItemInput) []int32 {
	seen := make(map[int32]struct{})
	out := make([]int32, 0)
	for _, it := range items {
		for _, oid := range it.OptionIDs {
			if _, ok := seen[oid]; ok {
				continue
			}
			seen[oid] = struct{}{}
			out = append(out, oid)
		}
	}
	return out
}

func generateCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i, v := range b {
		code[i] = codeChars[int(v)%len(codeChars)]
	}
	return string(code), nil
}
