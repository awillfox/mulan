package service

import (
	"errors"
	"sync"

	"mulan/internal/menucategory/domain"
)

type CategoryService struct {
	mu         sync.Mutex
	categories []domain.Category
	nextID     int
}

func NewCategoryService() *CategoryService {
	return &CategoryService{
		categories: []domain.Category{
			{ID: 1, Name: "Coffee"},
			{ID: 2, Name: "Cold"},
		},
		nextID: 3,
	}
}

func (s *CategoryService) List() []domain.Category {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Category, len(s.categories))
	copy(out, s.categories)
	return out
}

func (s *CategoryService) Create(name string) domain.Category {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := domain.Category{ID: s.nextID, Name: name}
	s.nextID++
	s.categories = append(s.categories, c)
	return c
}

func (s *CategoryService) Update(id int, name string) (domain.Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.categories {
		if c.ID == id {
			s.categories[i].Name = name
			return s.categories[i], nil
		}
	}
	return domain.Category{}, errors.New("category not found")
}

func (s *CategoryService) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.categories {
		if c.ID == id {
			s.categories = append(s.categories[:i], s.categories[i+1:]...)
			return nil
		}
	}
	return errors.New("category not found")
}
