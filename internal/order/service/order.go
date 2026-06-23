package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	ErrAlreadyPaid       = errors.New("order already paid")
	ErrNoItems           = errors.New("no items")
	ErrUnknownMenu       = errors.New("unknown menu")
	ErrMenuInactive      = errors.New("menu inactive")
	ErrInvalidOption     = errors.New("option not allowed for menu")
	ErrUnknownOption     = errors.New("unknown option")
	ErrMissingRequired   = errors.New("missing required option")
	ErrMissingBaseOption = errors.New("missing base option")
	ErrInvalidBaseOption = errors.New("invalid base option for menu")
	ErrOrderNotFound     = errors.New("order not found")
	ErrNotHeld           = errors.New("order is not held")
	ErrCannotHold        = errors.New("order cannot be held")
	ErrUnknownDiscount   = errors.New("unknown discount")
	ErrDiscountInactive  = errors.New("discount inactive")
	ErrShortTender       = errors.New("cash tendered is less than amount due")
	ErrChangeNotMakeable = errors.New("cannot make change from drawer")
)

// CashDrawer is the subset of cashdrawer/service used at checkout. Defined here
// (consumer side) to avoid a hard import cycle and keep the dependency explicit.
type CashDrawer interface {
	MakeChange(ctx context.Context, changeSatang int64) (map[int64]int, error)
	ApplyCashSale(ctx context.Context, q *sqlc.Queries, tender, change map[int64]int, actor string) error
}

type OrderService struct {
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	settings *settingsservice.SettingsService
	drawer   CashDrawer
}

func NewOrderService(pool *pgxpool.Pool, q *sqlc.Queries, settings *settingsservice.SettingsService, drawer CashDrawer) *OrderService {
	return &OrderService{pool: pool, q: q, settings: settings, drawer: drawer}
}

type CreatedOrder struct {
	Code string
	ID   int32
}

func (s *OrderService) Create(ctx context.Context) (CreatedOrder, error) {
	code, err := generateCode()
	if err != nil {
		return CreatedOrder{}, fmt.Errorf("generate code: %w", err)
	}
	row, err := s.q.CreateOrder(ctx, code)
	if err != nil {
		return CreatedOrder{}, fmt.Errorf("create order: %w", err)
	}
	return CreatedOrder{Code: row.Code, ID: row.ID}, nil
}

// CheckoutItemInput is what the handler accepts from the client. The server
// only trusts MenuID, Qty, OptionIDs, and DiscountIDs — Name and Price are
// looked up server-side from the menus/options tables.
type CheckoutItemInput struct {
	MenuID       int32
	Qty          int32
	OptionIDs    []int32
	DiscountIDs  []int32 // per-line discounts applied to this line
	BaseOptionID int32   // chosen base option (0 = none)
}

// CustomerInput is the optional membership capture at checkout. An empty Phone
// means "no member" and the whole loyalty path is skipped.
type CustomerInput struct {
	Phone string
	Name  string
}

// CashPayment carries the per-denomination tender for a cash sale. Empty (nil or
// IsCash=false) means a non-cash sale and the drawer is untouched. Tender is
// satang-keyed denomination -> count.
type CashPayment struct {
	IsCash bool
	Tender map[int64]int
	Actor  string
}

// Checkout finalises an open order: it validates every line against the
// authoritative menu/option data, applies per-line and whole-order discounts,
// persists order_items + order_item_options + order_discounts, marks the order
// paid, and returns totals. The whole flow runs in a single transaction so any
// failure rolls back cleanly. Calling Checkout on an already-paid order returns
// ErrAlreadyPaid so the caller can map it to 409 instead of double-charging.
//
// Discounts are applied before VAT: per-line discounts reduce each line, then
// whole-order discounts reduce the net subtotal, then VAT is computed on the
// discounted subtotal. Every applied discount is clamped so no line — and the
// order total — can never go below zero.
func (s *OrderService) Checkout(ctx context.Context, code string, items []CheckoutItemInput, orderDiscountIDs []int32, customer CustomerInput, cash CashPayment) (*domain.CheckoutResult, error) {
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
	baseOptsByMenu, err := loadBaseOptions(ctx, q, menuByID)
	if err != nil {
		return nil, err
	}
	discByID, err := loadDiscounts(ctx, q, items, orderDiscountIDs)
	if err != nil {
		return nil, err
	}

	resultItems := make([]domain.CheckoutResultItem, 0, len(items))
	applied := make([]domain.AppliedDiscount, 0)
	var subtotalSatang, itemDiscountSatang int64
	var normalItemSatang, subsidyItemSatang int64

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

		basePrice, baseName, err := resolveLineBase(m.Price, baseOptsByMenu[m.ID], in.BaseOptionID)
		if err != nil {
			return nil, err
		}

		unitPrice := basePrice + deltaSum
		lineTotal := unitPrice * int64(in.Qty)
		subtotalSatang += lineTotal

		itemID, err := q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID:        order.ID,
			MenuID:         pgtype.Int4{Int32: m.ID, Valid: true},
			Name:           m.Name,
			Price:          basePrice,
			Qty:            in.Qty,
			BaseOptionName: textOrNull(baseName),
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

		// Per-line discounts. Percentage discounts are computed against the
		// line's own total so stacking order doesn't matter; the running
		// remainder clamp keeps the line from going negative.
		lineRemaining := lineTotal
		for _, did := range dedupIDs(in.DiscountIDs) {
			d := discByID[did]
			amt := computeDiscountAmount(d, lineTotal)
			if amt > lineRemaining {
				amt = lineRemaining
			}
			if amt <= 0 {
				continue
			}
			lineRemaining -= amt
			itemDiscountSatang += amt
			if d.IsSubsidy {
				subsidyItemSatang += amt
			} else {
				normalItemSatang += amt
			}
			if err := q.CreateOrderDiscount(ctx, sqlc.CreateOrderDiscountParams{
				OrderID:      order.ID,
				OrderItemID:  pgtype.Int4{Int32: itemID, Valid: true},
				DiscountID:   pgtype.Int4{Int32: d.ID, Valid: true},
				Name:         d.Name,
				DiscountType: d.DiscountType,
				Amount:       amt,
				IsSubsidy:    d.IsSubsidy,
			}); err != nil {
				return nil, fmt.Errorf("insert order discount: %w", err)
			}
			applied = append(applied, domain.AppliedDiscount{DiscountID: d.ID, Name: d.Name, Type: d.DiscountType, Amount: amt, IsSubsidy: d.IsSubsidy})
		}

		resultItems = append(resultItems, domain.CheckoutResultItem{
			Name:           m.Name,
			Price:          basePrice,
			Qty:            in.Qty,
			Options:        opts,
			BaseOptionName: baseName,
		})
	}

	// Whole-order discounts apply against the subtotal net of per-line
	// discounts; percentage discounts use that net base, and the remainder
	// clamp keeps the order total from going below zero.
	subtotalAfterItem := subtotalSatang - itemDiscountSatang
	orderRemaining := subtotalAfterItem
	var normalOrderSatang, subsidyOrderSatang int64
	for _, did := range dedupIDs(orderDiscountIDs) {
		d := discByID[did]
		amt := computeDiscountAmount(d, subtotalAfterItem)
		if amt > orderRemaining {
			amt = orderRemaining
		}
		if amt <= 0 {
			continue
		}
		orderRemaining -= amt
		if d.IsSubsidy {
			subsidyOrderSatang += amt
		} else {
			normalOrderSatang += amt
		}
		if err := q.CreateOrderDiscount(ctx, sqlc.CreateOrderDiscountParams{
			OrderID:      order.ID,
			OrderItemID:  pgtype.Int4{Valid: false},
			DiscountID:   pgtype.Int4{Int32: d.ID, Valid: true},
			Name:         d.Name,
			DiscountType: d.DiscountType,
			Amount:       amt,
			IsSubsidy:    d.IsSubsidy,
		}); err != nil {
			return nil, fmt.Errorf("insert order discount: %w", err)
		}
		applied = append(applied, domain.AppliedDiscount{DiscountID: d.ID, Name: d.Name, Type: d.DiscountType, Amount: amt, IsSubsidy: d.IsSubsidy})
	}

	cfg := s.settings.Get()
	t := computeOrderTotals(
		subtotalSatang,
		normalItemSatang+normalOrderSatang,
		subsidyItemSatang+subsidyOrderSatang,
		cfg.VatPercent,
	)
	totalSatang := t.CustomerPays

	// Cash payment: round the amount due to whole baht, validate tender, compute
	// the change breakdown against live drawer stock, and apply the movement in
	// this same transaction so the drawer can never drift from the sale.
	var roundedDueSatang, changeSatang int64
	var changeBreakdown map[int64]int
	if cash.IsCash {
		roundedDueSatang = roundToBaht(totalSatang)
		var tenderSatang int64
		for d, c := range cash.Tender {
			tenderSatang += d * int64(c)
		}
		if tenderSatang < roundedDueSatang {
			return nil, ErrShortTender
		}
		changeSatang = tenderSatang - roundedDueSatang
		changeBreakdown, err = s.drawer.MakeChange(ctx, changeSatang)
		if err != nil {
			return nil, ErrChangeNotMakeable
		}
		if err := s.drawer.ApplyCashSale(ctx, q, cash.Tender, changeBreakdown, cash.Actor); err != nil {
			return nil, fmt.Errorf("apply cash sale: %w", err)
		}
	}

	var memberID pgtype.Int4
	var pointsEarned int64
	var hasMember bool
	var memberName, memberPhone string
	var pointsBalance int64

	if p := strings.TrimSpace(customer.Phone); p != "" {
		member, err := q.FindMemberByPhone(ctx, p)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("find member: %w", err)
			}
			var nameArg pgtype.Text
			if n := strings.TrimSpace(customer.Name); n != "" {
				nameArg = pgtype.Text{String: n, Valid: true}
			}
			member, err = q.CreateMember(ctx, sqlc.CreateMemberParams{
				Phone: p,
				Name:  nameArg,
			})
			if err != nil {
				return nil, fmt.Errorf("create member: %w", err)
			}
		}
		pointsEarned = int64(float64(totalSatang) / 100 * cfg.PointsPerBaht)
		updated, err := q.AddMemberPoints(ctx, sqlc.AddMemberPointsParams{
			Delta: pointsEarned,
			ID:    member.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("add member points: %w", err)
		}
		memberID = pgtype.Int4{Int32: member.ID, Valid: true}
		hasMember = true
		memberPhone = member.Phone
		if member.Name.Valid {
			memberName = member.Name.String
		}
		pointsBalance = updated.Points
	}

	if err := q.PayOrder(ctx, sqlc.PayOrderParams{
		MemberID:     memberID,
		PointsEarned: pointsEarned,
		Code:         code,
	}); err != nil {
		return nil, fmt.Errorf("pay order: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &domain.CheckoutResult{
		OrderID:       order.ID,
		Code:          code,
		Subtotal:      float64(t.Gross) / 100,
		Discount:      float64(t.NormalDisc) / 100,
		Subsidy:       float64(t.Subsidy) / 100,
		VAT:           float64(t.VAT) / 100,
		VATPercent:    cfg.VatPercent,
		ShopName:      cfg.ShopName,
		ReceiptFooter: cfg.ReceiptFooter,
		Total:         float64(t.CustomerPays) / 100,
		Items:         resultItems,
		Discounts:     applied,
		HasMember:     hasMember,
		MemberName:    memberName,
		MemberPhone:   memberPhone,
		PointsEarned:  pointsEarned,
		PointsBalance: pointsBalance,

		RoundedDue:      float64(roundedDueSatang) / 100,
		Change:          float64(changeSatang) / 100,
		ChangeBreakdown: breakdownToStringMap(changeBreakdown),
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

// loadBaseOptions returns the base options attached to each menu being ordered,
// keyed by menu id. Menus with no base options simply have no entry.
func loadBaseOptions(ctx context.Context, q *sqlc.Queries, menus map[int32]sqlc.Menu) (map[int32][]sqlc.MenuBaseOption, error) {
	ids := make([]int32, 0, len(menus))
	for id := range menus {
		ids = append(ids, id)
	}
	out := make(map[int32][]sqlc.MenuBaseOption, len(menus))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListBaseOptionsByMenuIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load base options: %w", err)
	}
	for _, b := range rows {
		out[b.MenuID] = append(out[b.MenuID], b)
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

// resolveLineBase returns the per-unit base price and snapshot name for a line.
// When the menu has base options exactly one valid baseOptionID is required and
// its absolute price becomes the line base. When the menu has none, baseOptionID
// must be zero and the base is the menu's own price.
func resolveLineBase(menuPrice int64, baseOpts []sqlc.MenuBaseOption, baseOptionID int32) (int64, string, error) {
	if len(baseOpts) == 0 {
		if baseOptionID != 0 {
			return 0, "", ErrInvalidBaseOption
		}
		return menuPrice, "", nil
	}
	if baseOptionID == 0 {
		return 0, "", ErrMissingBaseOption
	}
	for _, b := range baseOpts {
		if b.ID == baseOptionID {
			return b.Price, b.Name, nil
		}
	}
	return 0, "", ErrInvalidBaseOption
}

// textOrNull wraps a snapshot string into pgtype.Text, mapping "" to SQL NULL.
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
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

// loadDiscounts fetches every discount referenced by the order (per-line and
// whole-order) and rejects any that doesn't exist or has been deactivated, so
// a client can't apply a stale or unknown discount.
func loadDiscounts(ctx context.Context, q *sqlc.Queries, items []CheckoutItemInput, orderDiscountIDs []int32) (map[int32]sqlc.Discount, error) {
	ids := uniqueDiscountIDs(items, orderDiscountIDs)
	if len(ids) == 0 {
		return map[int32]sqlc.Discount{}, nil
	}
	rows, err := q.GetDiscountsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load discounts: %w", err)
	}
	out := make(map[int32]sqlc.Discount, len(rows))
	for _, d := range rows {
		out[d.ID] = d
	}
	for _, id := range ids {
		d, ok := out[id]
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownDiscount, id)
		}
		if !d.Active {
			return nil, fmt.Errorf("%w: %d", ErrDiscountInactive, id)
		}
	}
	return out, nil
}

// computeDiscountAmount returns the satang reduction d applies to base. For a
// percentage discount d.Value is hundredths-of-a-percent (10% => 1000). A
// non-whole-baht result is rounded UP to the next whole baht (satang coins
// are impractical at the till). The result is clamped into [0, base].
func computeDiscountAmount(d sqlc.Discount, base int64) int64 {
	if base <= 0 {
		return 0
	}
	var amt int64
	if d.DiscountType == "percent" {
		amt = base * d.Value / 10000
	} else {
		amt = d.Value // fixed, satang
	}
	// Round any leftover satang up to a whole baht (100 satang).
	if r := amt % 100; r != 0 {
		amt += 100 - r
	}
	if amt > base {
		amt = base
	}
	if amt < 0 {
		amt = 0
	}
	return amt
}

func uniqueDiscountIDs(items []CheckoutItemInput, orderDiscountIDs []int32) []int32 {
	seen := make(map[int32]struct{})
	out := make([]int32, 0)
	add := func(ids []int32) {
		for _, id := range ids {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	for _, it := range items {
		add(it.DiscountIDs)
	}
	add(orderDiscountIDs)
	return out
}

// dedupIDs drops duplicate and non-positive IDs, preserving first-seen order.
func dedupIDs(ids []int32) []int32 {
	seen := make(map[int32]struct{}, len(ids))
	out := make([]int32, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
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

// roundToBaht rounds a satang amount to the nearest whole baht (100 satang).
// Cash is settled in whole baht because the smallest tracked coin is ฿1.
func roundToBaht(satang int64) int64 {
	r := satang % 100
	if r == 0 {
		return satang
	}
	if r >= 50 {
		return satang - r + 100
	}
	return satang - r
}

// breakdownToStringMap converts a satang-keyed change breakdown to the
// string-keyed shape the JSON API uses (nil-safe → empty map).
func breakdownToStringMap(b map[int64]int) map[string]int {
	out := make(map[string]int, len(b))
	for d, n := range b {
		out[strconv.FormatInt(d, 10)] = n
	}
	return out
}
