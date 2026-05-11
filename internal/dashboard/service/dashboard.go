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
	Revenue       float64 `json:"revenue"`
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
	stats := PeriodStats{
		Revenue: float64(row.Revenue) / 100,
		Orders:  row.Orders,
		Items:   row.Items,
	}
	if row.Orders > 0 {
		stats.AvgTicket = stats.Revenue / float64(row.Orders)
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

func (s *DashboardService) TopMenus(ctx context.Context, from, to time.Time) ([]TopMenuItem, error) {
	rows, err := s.q.TopMenusBySales(ctx, sqlc.TopMenusBySalesParams{
		FromAt: pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("top menus: %w", err)
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
