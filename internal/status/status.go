// Package status is NeoRouter's public status surface: the readiness report
// behind /api/public/status (server/public_status.js), its 48-hour history
// (server/status_history.js), and the analytics reporting of
// server/analytics.js, analytics-config.js, analytics-client.js and
// host_analytics.js.
//
// The report is unauthenticated, so it says nothing about providers, models or
// accounts beyond whether the database and the model catalog answer a query:
// same product name, same component texts, same JSON keys as the Node server.
package status

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Component is one monitored service of the status page.
type Component struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// PublicStatus is buildPublicStatus() from server/public_status.js.
type PublicStatus struct {
	Product             string      `json:"product"`
	Status              string      `json:"status"`
	UpdatedAt           string      `json:"updatedAt"`
	RefreshAfterSeconds int         `json:"refreshAfterSeconds"`
	Note                string      `json:"note"`
	Components          []Component `json:"components"`
}

// Response is GET /api/public/status: the report with its history appended,
// in the order app.js serialises them.
type Response struct {
	PublicStatus
	History []Sample `json:"history"`
}

// Service builds the public status report.
//
// Storage and Catalog are the two probes the report publishes. When they are
// nil the checks run against DB, which is enough for a single-database install;
// internal/app passes the catalog domain's own probe instead of querying its
// tables from here.
type Service struct {
	DB      *dbx.DB
	Storage func(context.Context) error
	Catalog func(context.Context) error
	History *History

	// Now overrides the clock in tests.
	Now func() time.Time
}

// New builds a Service over dir for the hourly history.
func New(db *dbx.DB, dir string) *Service {
	return &Service{DB: db, History: NewHistory(dir)}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Public is server/public_status.js buildPublicStatus: four components whose
// status decides the overall one.
func (s *Service) Public(ctx context.Context) PublicStatus {
	storage, catalog := s.probes(ctx)
	components := []Component{
		{Group: "Core", Name: "Web App & API", Status: "operational",
			Description: "The web server is responding to requests."},
		{Group: "Core", Name: "Inference Gateway", Status: pick(storage, "operational", "degraded"),
			Description: pick(storage, "Gateway routes are available. Individual providers are not tested.", "The gateway depends on unavailable storage.")},
		{Group: "Data", Name: "Storage", Status: pick(storage, "operational", "unavailable"),
			Description: pick(storage, "The database is responding.", "The database could not be reached.")},
		{Group: "Routing", Name: "Model Catalog", Status: pick(catalog, "operational", "degraded"),
			Description: pick(catalog, "The model catalog can be queried.", "The model catalog could not be queried.")},
	}
	return PublicStatus{
		Product:             "NeoRouter",
		Status:              overall(components),
		UpdatedAt:           isoMillis(s.now()),
		RefreshAfterSeconds: 60,
		Note:                note(overall(components)),
		Components:          components,
	}
}

// Report is the body of GET /api/public/status.
func (s *Service) Report(ctx context.Context) Response {
	hist := s.History
	if hist == nil {
		hist = NewHistory("")
	}
	return Response{PublicStatus: s.Public(ctx), History: hist.History()}
}

// Routes registers the unauthenticated public status endpoint.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/public/status", func(w http.ResponseWriter, r *http.Request) {
		// A status page must never be served from a cache.
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, s.Report(r.Context()))
	})
}

// probes runs the two checks in the order public_status.js does: a failed
// storage check leaves the catalog untested.
func (s *Service) probes(ctx context.Context) (storage, catalog bool) {
	if err := s.storageCheck(ctx); err != nil {
		return false, false
	}
	if err := s.catalogCheck(ctx); err != nil {
		return true, false
	}
	return true, true
}

func (s *Service) storageCheck(ctx context.Context) error {
	if s.Storage != nil {
		return s.Storage(ctx)
	}
	if s.DB == nil {
		return fmt.Errorf("status: storage check: no database")
	}
	var one int
	return s.DB.R.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

func (s *Service) catalogCheck(ctx context.Context) error {
	if s.Catalog != nil {
		return s.Catalog(ctx)
	}
	if s.DB == nil {
		return fmt.Errorf("status: catalog check: no database")
	}
	var one int
	err := s.DB.R.QueryRowContext(ctx, `SELECT 1 FROM models LIMIT 1`).Scan(&one)
	// An empty catalog is a reachable catalog: better-sqlite3's .get() returns
	// undefined for no row and does not throw.
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func pick(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func overall(components []Component) string {
	state := "operational"
	for _, c := range components {
		switch c.Status {
		case "unavailable":
			return "unavailable"
		case "degraded":
			state = "degraded"
		}
	}
	return state
}

func note(status string) string {
	if status == "operational" {
		return "All monitored services are operational."
	}
	return "One or more monitored services need attention."
}

// isoMillis is JavaScript's Date#toISOString: UTC, millisecond precision.
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
