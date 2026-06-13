package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/Rhymond/go-money"

	"mulan/internal/cashdrawer/service"
	cashiersvc "mulan/internal/cashier/service"
	"mulan/internal/response"
)

// denomsResponse reports current counts (satang-keyed via string) and the derived
// total in THB.
type denomsResponse struct {
	Counts map[string]int `json:"counts"`
	Total  float64        `json:"total"`
}

func toDenomsResponse(counts map[int64]int, totalSatang int64) denomsResponse {
	out := denomsResponse{Counts: make(map[string]int, len(counts))}
	for d, c := range counts {
		out.Counts[itoa(d)] = c
	}
	out.Total = money.New(totalSatang, money.THB).AsMajorUnits()
	return out
}

func (h *Handler) getDenominations(w http.ResponseWriter, r *http.Request) {
	counts, total, err := h.svc.CurrentDenoms(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to read denominations", err)
		return
	}
	response.OK(w, r, toDenomsResponse(counts, total))
}

type denomWriteRequest struct {
	CashierID int32          `json:"cashier_id"`
	PIN       string         `json:"pin"`
	Counts    map[string]int `json:"counts"` // for PUT (absolute)
	Delta     map[string]int `json:"delta"`  // for adjust (relative)
}

// authorize verifies the request's cashier_id + pin belong to an active manager
// and returns the actor name to stamp on the audit row.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, id int32, pin string) (string, bool) {
	c, err := h.verifier.VerifyManager(r.Context(), id, pin)
	if err != nil {
		if errors.Is(err, cashiersvc.ErrInvalidCredentials) || errors.Is(err, cashiersvc.ErrNotManager) {
			response.Error(w, r, http.StatusForbidden, "manager id + PIN required", err)
			return "", false
		}
		response.Error(w, r, http.StatusInternalServerError, "authorization failed", err)
		return "", false
	}
	return c.Name, true
}

func (h *Handler) setDenominations(w http.ResponseWriter, r *http.Request) {
	var req denomWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	actor, ok := h.authorize(w, r, req.CashierID, req.PIN)
	if !ok {
		return
	}
	counts, err := parseDenomMap(req.Counts)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	newCounts, total, err := h.svc.SetDenoms(r.Context(), counts, actor)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnknownDenomination), errors.Is(err, service.ErrNegativeCount):
			response.Error(w, r, http.StatusBadRequest, "invalid denominations", err)
		default:
			response.Error(w, r, http.StatusInternalServerError, "failed to set denominations", err)
		}
		return
	}
	response.OK(w, r, toDenomsResponse(newCounts, total))
}

func (h *Handler) adjustDenominations(w http.ResponseWriter, r *http.Request) {
	var req denomWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	actor, ok := h.authorize(w, r, req.CashierID, req.PIN)
	if !ok {
		return
	}
	delta, err := parseDenomMap(req.Delta)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	newCounts, total, err := h.svc.AdjustDenoms(r.Context(), delta, actor)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnknownDenomination):
			response.Error(w, r, http.StatusBadRequest, "invalid denominations", err)
		case errors.Is(err, service.ErrInsufficientStock):
			response.Error(w, r, http.StatusConflict, "insufficient stock for adjustment", err)
		default:
			response.Error(w, r, http.StatusInternalServerError, "adjust failed", err)
		}
		return
	}
	response.OK(w, r, toDenomsResponse(newCounts, total))
}

type changePreviewRequest struct {
	Due    float64        `json:"due"`    // THB amount due (POS estimate)
	Tender map[string]int `json:"tender"` // denom satang string -> count
}

type changePreviewResponse struct {
	RoundedDue  float64        `json:"rounded_due"`
	ChangeTotal float64        `json:"change_total"`
	Breakdown   map[string]int `json:"breakdown"`
	Makeable    bool           `json:"makeable"`
}

func (h *Handler) changePreview(w http.ResponseWriter, r *http.Request) {
	var req changePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Due < 0 {
		response.Error(w, r, http.StatusBadRequest, "due must be >= 0", nil)
		return
	}
	tender, err := parseDenomMap(req.Tender)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	roundedDue := roundToBahtSatang(satangFromTHB(req.Due))
	var tenderSatang int64
	for d, c := range tender {
		tenderSatang += d * int64(c)
	}
	out := changePreviewResponse{
		RoundedDue: money.New(roundedDue, money.THB).AsMajorUnits(),
		Breakdown:  map[string]int{},
	}
	if tenderSatang < roundedDue {
		out.Makeable = false
		out.ChangeTotal = money.New(tenderSatang-roundedDue, money.THB).AsMajorUnits()
		response.OK(w, r, out)
		return
	}
	changeSatang := tenderSatang - roundedDue
	out.ChangeTotal = money.New(changeSatang, money.THB).AsMajorUnits()
	breakdown, err := h.svc.MakeChange(r.Context(), changeSatang)
	if err != nil {
		out.Makeable = false
		response.OK(w, r, out)
		return
	}
	out.Makeable = true
	for d, n := range breakdown {
		out.Breakdown[itoa(d)] = n
	}
	response.OK(w, r, out)
}

// roundToBahtSatang rounds a satang amount to the nearest whole baht (100 satang).
func roundToBahtSatang(satang int64) int64 {
	return int64(math.Round(float64(satang)/100.0)) * 100
}

func itoa(d int64) string { return strconv.FormatInt(d, 10) }

// parseDenomMap converts a JSON object keyed by satang strings into an int64-keyed
// map, rejecting non-numeric keys and keys that are not tracked denominations.
func parseDenomMap(in map[string]int) (map[int64]int, error) {
	out := make(map[int64]int, len(in))
	for k, v := range in {
		d, err := strconv.ParseInt(k, 10, 64)
		if err != nil || !trackedDenom(d) {
			return nil, errInvalidDenomKey
		}
		out[d] = v
	}
	return out, nil
}

func trackedDenom(d int64) bool {
	for _, x := range service.DenominationsSatang {
		if x == d {
			return true
		}
	}
	return false
}

var errInvalidDenomKey = errors.New("invalid denomination key")
