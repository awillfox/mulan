package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/sqlc"
)

var ErrUnknownGroup = errors.New("unknown option group")

type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewService(pool *pgxpool.Pool, q *sqlc.Queries) *Service {
	return &Service{pool: pool, q: q}
}

type GroupWithOptions struct {
	Group   sqlc.OptionGroup
	Options []sqlc.Option
}

func (s *Service) ListGroups(ctx context.Context) ([]GroupWithOptions, error) {
	groups, err := s.q.ListOptionGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	if len(groups) == 0 {
		return []GroupWithOptions{}, nil
	}

	ids := make([]int32, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	opts, err := s.q.ListOptionsByGroups(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}

	byGroup := make(map[int32][]sqlc.Option, len(groups))
	for _, o := range opts {
		byGroup[o.OptionGroupID] = append(byGroup[o.OptionGroupID], o)
	}

	out := make([]GroupWithOptions, len(groups))
	for i, g := range groups {
		out[i] = GroupWithOptions{Group: g, Options: byGroup[g.ID]}
	}
	return out, nil
}

func (s *Service) CreateGroup(ctx context.Context, name, mode string) (sqlc.OptionGroup, error) {
	return s.q.CreateOptionGroup(ctx, sqlc.CreateOptionGroupParams{Name: name, SelectionMode: mode})
}

func (s *Service) UpdateGroup(ctx context.Context, id int32, name, mode string) (sqlc.OptionGroup, error) {
	return s.q.UpdateOptionGroup(ctx, sqlc.UpdateOptionGroupParams{ID: id, Name: name, SelectionMode: mode})
}

func (s *Service) DeleteGroup(ctx context.Context, id int32) error {
	return s.q.DeleteOptionGroup(ctx, id)
}

func (s *Service) CreateOption(ctx context.Context, groupID int32, name string, priceDelta int64, sortOrder int32) (sqlc.Option, error) {
	return s.q.CreateOption(ctx, sqlc.CreateOptionParams{
		OptionGroupID: groupID,
		Name:          name,
		PriceDelta:    priceDelta,
		SortOrder:     sortOrder,
	})
}

func (s *Service) UpdateOption(ctx context.Context, id int32, name string, priceDelta int64, sortOrder int32) (sqlc.Option, error) {
	return s.q.UpdateOption(ctx, sqlc.UpdateOptionParams{
		ID:         id,
		Name:       name,
		PriceDelta: priceDelta,
		SortOrder:  sortOrder,
	})
}

func (s *Service) DeleteOption(ctx context.Context, id int32) error {
	return s.q.DeleteOption(ctx, id)
}

// SetMenuGroups replaces the menu's attached option groups. Runs in a
// transaction so a failure during re-insert can't leave the menu with zero
// attached groups. Validates every requested group exists before mutating
// anything so an invalid ID rolls back without touching menu_option_groups.
func (s *Service) SetMenuGroups(ctx context.Context, menuID int32, groupIDs []int32) error {
	dedup := make([]int32, 0, len(groupIDs))
	seen := make(map[int32]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		dedup = append(dedup, id)
	}

	if len(dedup) > 0 {
		found, err := s.q.GetOptionGroupsByIDs(ctx, dedup)
		if err != nil {
			return fmt.Errorf("validate groups: %w", err)
		}
		if len(found) != len(dedup) {
			return ErrUnknownGroup
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	if err := q.ClearMenuOptionGroups(ctx, menuID); err != nil {
		return fmt.Errorf("clear: %w", err)
	}
	for i, gid := range dedup {
		if err := q.AttachMenuOptionGroup(ctx, sqlc.AttachMenuOptionGroupParams{
			MenuID:        menuID,
			OptionGroupID: gid,
			SortOrder:     int32(i),
		}); err != nil {
			return fmt.Errorf("attach: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// GroupsForMenus loads the option groups (with their options) attached to
// each of the given menus. Only the relevant groups are pulled — earlier
// versions read every option_group row regardless of filter.
func (s *Service) GroupsForMenus(ctx context.Context, menuIDs []int32) (map[int32][]GroupWithOptions, error) {
	if len(menuIDs) == 0 {
		return map[int32][]GroupWithOptions{}, nil
	}
	links, err := s.q.ListMenuOptionGroupLinks(ctx, menuIDs)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	if len(links) == 0 {
		return map[int32][]GroupWithOptions{}, nil
	}

	groupIDSet := make(map[int32]struct{}, len(links))
	for _, l := range links {
		groupIDSet[l.OptionGroupID] = struct{}{}
	}
	groupIDs := make([]int32, 0, len(groupIDSet))
	for id := range groupIDSet {
		groupIDs = append(groupIDs, id)
	}

	groups, err := s.q.GetOptionGroupsByIDs(ctx, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("get groups: %w", err)
	}
	byID := make(map[int32]sqlc.OptionGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}

	opts, err := s.q.ListOptionsByGroups(ctx, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	optsByGroup := make(map[int32][]sqlc.Option, len(groupIDs))
	for _, o := range opts {
		optsByGroup[o.OptionGroupID] = append(optsByGroup[o.OptionGroupID], o)
	}

	out := make(map[int32][]GroupWithOptions, len(menuIDs))
	for _, l := range links {
		g, ok := byID[l.OptionGroupID]
		if !ok {
			continue
		}
		out[l.MenuID] = append(out[l.MenuID], GroupWithOptions{Group: g, Options: optsByGroup[g.ID]})
	}
	return out, nil
}
