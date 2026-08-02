package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

// shopTZ is the IANA timezone used for day/hour bucketing in reports.
// Receipts and analytics are presented in this timezone regardless of where
// the DB session timezone is.
const shopTZ = "Asia/Bangkok"

type DashboardService struct {
	q *sqlc.Queries
}

func NewDashboardService(q *sqlc.Queries) *DashboardService {
	return &DashboardService{q: q}
}

type TodaySummary struct {
	Sales  float64 `json:"sales"`
	Orders int64   `json:"orders"`
}

func (s *DashboardService) TodaySummary(ctx context.Context) (*TodaySummary, error) {
	sales, err := s.q.SumTodaySales(ctx)
	if err != nil {
		return nil, fmt.Errorf("sum today sales: %w", err)
	}
	orders, err := s.q.CountTodayOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("count today orders: %w", err)
	}
	return &TodaySummary{
		Sales:  float64(sales) / 100,
		Orders: orders,
	}, nil
}

type TopMenuItem struct {
	Name    string  `json:"name"`
	QtySold int64   `json:"qty_sold"`
	Revenue float64 `json:"revenue"`
}

type DayPoint struct {
	Day     string  `json:"day"` // ISO date YYYY-MM-DD
	Revenue float64 `json:"revenue"`
	Orders  int64   `json:"orders"`
	Items   int64   `json:"items"`
}

func (s *DashboardService) SalesByDay(ctx context.Context, from, to time.Time) ([]DayPoint, error) {
	rows, err := s.q.SalesByDay(ctx, sqlc.SalesByDayParams{
		Tz:     shopTZ,
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("sales by day: %w", err)
	}
	out := make([]DayPoint, len(rows))
	for i, r := range rows {
		out[i] = DayPoint{
			Day:     r.Day.Time.Format("2006-01-02"),
			Revenue: float64(r.Revenue) / 100,
			Orders:  r.Orders,
			Items:   r.Items,
		}
	}
	return out, nil
}

type HeatmapCell struct {
	DOW     int32   `json:"dow"`  // 0=Sunday … 6=Saturday (Postgres EXTRACT DOW)
	Hour    int32   `json:"hour"` // 0–23, shop-local
	Revenue float64 `json:"revenue"`
	Orders  int64   `json:"orders"`
}

func (s *DashboardService) Heatmap(ctx context.Context, from, to time.Time) ([]HeatmapCell, error) {
	rows, err := s.q.SalesByHourDOW(ctx, sqlc.SalesByHourDOWParams{
		Tz:     shopTZ,
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("heatmap: %w", err)
	}
	out := make([]HeatmapCell, len(rows))
	for i, r := range rows {
		out[i] = HeatmapCell{
			DOW:     r.Dow,
			Hour:    r.Hour,
			Revenue: float64(r.Revenue) / 100,
			Orders:  r.Orders,
		}
	}
	return out, nil
}

type PeriodStats struct {
	Revenue       float64 `json:"revenue"`        // == NetSales (back-compat alias)
	Gross         float64 `json:"gross"`          // sum of price*qty (VAT-inclusive)
	Discount      float64 `json:"discount"`       // normal discounts given
	Subsidy       float64 `json:"subsidy"`        // sponsor-covered
	NetSales      float64 `json:"net_sales"`      // Gross - Discount (what the shop earns)
	CustomersPaid float64 `json:"customers_paid"` // Gross - Discount - Subsidy
	Orders        int64   `json:"orders"`
	Items         int64   `json:"items"`
	AvgTicket     float64 `json:"avg_ticket"`
	ItemsPerOrder float64 `json:"items_per_order"`
}

type CompareResult struct {
	Current  PeriodStats `json:"current"`
	Previous PeriodStats `json:"previous"`
}

func (s *DashboardService) periodStats(ctx context.Context, from, to time.Time) (PeriodStats, error) {
	row, err := s.q.PeriodSummary(ctx, sqlc.PeriodSummaryParams{
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return PeriodStats{}, fmt.Errorf("period summary: %w", err)
	}
	disc, err := s.q.DiscountSummary(ctx, sqlc.DiscountSummaryParams{
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return PeriodStats{}, fmt.Errorf("discount summary: %w", err)
	}
	gross := float64(row.Revenue) / 100
	discount := float64(disc.Discount) / 100
	subsidy := float64(disc.Subsidy) / 100
	net := gross - discount
	stats := PeriodStats{
		Revenue:       net,
		Gross:         gross,
		Discount:      discount,
		Subsidy:       subsidy,
		NetSales:      net,
		CustomersPaid: net - subsidy,
		Orders:        row.Orders,
		Items:         row.Items,
	}
	if row.Orders > 0 {
		stats.AvgTicket = net / float64(row.Orders)
		stats.ItemsPerOrder = float64(row.Items) / float64(row.Orders)
	}
	return stats, nil
}

func (s *DashboardService) Compare(ctx context.Context, from, to time.Time) (*CompareResult, error) {
	current, err := s.periodStats(ctx, from, to)
	if err != nil {
		return nil, err
	}
	span := to.Sub(from)
	prevTo := from
	prevFrom := from.Add(-span)
	previous, err := s.periodStats(ctx, prevFrom, prevTo)
	if err != nil {
		return nil, err
	}
	return &CompareResult{Current: current, Previous: previous}, nil
}

// menuSales aggregates paid order items by name over [from, to), newest
// sales included, ordered by quantity descending. A rowLimit that is not
// Valid means SQL NULL, which Postgres reads as "no limit".
func (s *DashboardService) menuSales(ctx context.Context, from, to time.Time, rowLimit pgtype.Int8) ([]TopMenuItem, error) {
	rows, err := s.q.TopMenusBySales(ctx, sqlc.TopMenusBySalesParams{
		FromAt:   pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:     pgtype.Timestamptz{Time: to, Valid: true},
		RowLimit: rowLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("menu sales: %w", err)
	}
	out := make([]TopMenuItem, len(rows))
	for i, r := range rows {
		out[i] = TopMenuItem{
			Name:    r.Name,
			QtySold: r.QtySold,
			Revenue: float64(r.Revenue) / 100,
		}
	}
	return out, nil
}

// topMenusLimit is how many items the dashboard's item-mix donut shows.
// More slices than this stops being readable on a phone.
const topMenusLimit = 10

// TopMenus backs the dashboard's item-mix donut.
func (s *DashboardService) TopMenus(ctx context.Context, from, to time.Time) ([]TopMenuItem, error) {
	return s.menuSales(ctx, from, to, pgtype.Int8{Int64: topMenusLimit, Valid: true})
}

// MenuItems backs the dashboard's "All items" list: every item sold in the
// window, no cap.
func (s *DashboardService) MenuItems(ctx context.Context, from, to time.Time) ([]TopMenuItem, error) {
	return s.menuSales(ctx, from, to, pgtype.Int8{})
}

type SubsidyProgram struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

func (s *DashboardService) SubsidyByProgram(ctx context.Context, from, to time.Time) ([]SubsidyProgram, error) {
	rows, err := s.q.SubsidyByProgram(ctx, sqlc.SubsidyByProgramParams{
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("subsidy by program: %w", err)
	}
	out := make([]SubsidyProgram, len(rows))
	for i, r := range rows {
		out[i] = SubsidyProgram{Name: r.Name, Amount: float64(r.Amount) / 100}
	}
	return out, nil
}
