package service

import "mulan/internal/menu/domain"

type MenuService struct{}

func NewMenuService() *MenuService {
	return &MenuService{}
}

func intPtr(v int) *int { return &v }

func (s *MenuService) List() []domain.Menu {
	return []domain.Menu{
		{ID: 1, Name: "Espresso", Price: 350, CategoryID: intPtr(1)},
		{ID: 2, Name: "Latte", Price: 450, CategoryID: intPtr(1)},
		{ID: 3, Name: "Cappuccino", Price: 400, CategoryID: intPtr(1)},
		{ID: 4, Name: "Americano", Price: 300, CategoryID: intPtr(1)},
		{ID: 5, Name: "Mocha", Price: 500, CategoryID: intPtr(1)},
		{ID: 6, Name: "Iced Tea", Price: 250, CategoryID: intPtr(2)},
		{ID: 7, Name: "Smoothie", Price: 450, CategoryID: intPtr(2)},
		{ID: 8, Name: "Water", Price: 100},
	}
}
