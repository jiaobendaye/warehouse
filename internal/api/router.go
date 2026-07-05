package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// Services bundles the four service handles a router needs. Construct it
// once at startup and pass it to NewRouter.
type Services struct {
	Accessory     *service.AccessoryService
	Stock         *service.StockService
	Flow          *service.FlowService
	Replenishment *service.ReplenishmentService
}

// RouterOptions controls cross-cutting middleware (currently just CORS).
type RouterOptions struct {
	AllowedOrigins []string
}

// NewRouter assembles the chi router, mounts every endpoint under /api/v1,
// and applies recoverer → logger → CORS in that order. Returns a fully
// configured http.Handler ready to be served.
func NewRouter(s Services, opts RouterOptions) http.Handler {
	r := chi.NewRouter()

	// Middleware chain: panic safety first, observability second, CORS last
	// (so cross-origin rejections still produce a clean log line).
	r.Use(Recoverer)
	r.Use(RequestLogger)
	r.Use(CORS(CORSOptions{AllowedOrigins: opts.AllowedOrigins}))

	acc := NewAccessoryHandler(s.Accessory)
	stk := NewStockHandler(s.Stock)
	flw := NewFlowHandler(s.Flow)
	rpl := NewReplenishmentHandler(s.Replenishment)

	r.Route("/api/v1", func(r chi.Router) {
		// Accessory CRUD
		r.Get("/accessories", acc.List)
		r.Post("/accessories", acc.Create)
		r.Get("/accessories/{id}", acc.Get)
		r.Patch("/accessories/{id}", acc.Update)
		r.Delete("/accessories/{id}", acc.Delete)

		// Stock movements
		r.Post("/stock/inbound", stk.Inbound)
		r.Post("/stock/outbound", stk.Outbound)
		r.Post("/stock/batch_inbound", stk.BatchInbound)
		r.Post("/stock/batch_outbound", stk.BatchOutbound)

		// Flow queries
		r.Get("/flows", flw.List)
		r.Get("/flows/{id}", flw.Get)

		// Replenishment advisor
		r.Get("/replenishment/scan", rpl.Scan)
		r.Post("/replenishment/check", rpl.Check)
	})

	return r
}
