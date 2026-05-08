package httpx

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func URLParamInt32(r *http.Request, key string) (int32, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return 0, fmt.Errorf("missing %s", key)
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return int32(v), nil
}

func DateQuery(r *http.Request, key string, loc *time.Location) (time.Time, bool, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return time.Time{}, false, nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return t, true, nil
}
