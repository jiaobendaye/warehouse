package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// AccessoryHandler exposes AccessoryService over HTTP.
type AccessoryHandler struct {
	svc *service.AccessoryService
}

func NewAccessoryHandler(svc *service.AccessoryService) *AccessoryHandler {
	return &AccessoryHandler{svc: svc}
}

// List — GET /api/v1/accessories?q=&limit=&offset=
func (h *AccessoryHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := parseIntQuery(r, "limit", 0)
	offset := parseIntQuery(r, "offset", 0)
	rows, total, err := h.svc.List(r.Context(), q, limit, offset)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  rows,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Create — POST /api/v1/accessories  body: Accessory (without ID/CreatedAt/UpdatedAt)
func (h *AccessoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in domain.Accessory
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	in.ID = 0 // ignore client-supplied id
	out, err := h.svc.Create(r.Context(), in)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// Get — GET /api/v1/accessories/{id}
func (h *AccessoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Get(r.Context(), id)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// Update — PATCH /api/v1/accessories/{id}  body: AccessoryUpdate (pointer fields).
func (h *AccessoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	var u domain.AccessoryUpdate
	if err := decodeJSON(r, &u); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := h.svc.Update(r.Context(), id, u)
	if err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Delete — DELETE /api/v1/accessories/{id}
func (h *AccessoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		status, code := TranslateError(err)
		WriteError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// --- shared helpers ------------------------------------------------------

// parseIDParam extracts a numeric {id} path param. On failure it writes a
// 400 and returns ok=false, so handlers can simply `return` on error.
func parseIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// parseIntQuery reads an integer query parameter. Empty/missing returns def.
// Non-numeric values return def silently — the service clamps limit/offset.
func parseIntQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// decodeJSON decodes the body into v, requiring Content-Type application/json.
// Failure (malformed JSON, wrong content-type, unknown fields when strict)
// yields a clean error.
func decodeJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !startsWith(ct, "application/json") {
		return errors.New("content-type must be application/json")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// writeJSON marshals v and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
