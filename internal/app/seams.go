package app

// The last seams: the request-log counters the processing page shows, the
// browser/host analytics, and the boot sequence (index.js).

import (
	"context"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/processing"
	"github.com/NeoLabs-Systems/neorouter/internal/status"
)

// processingStats is the /api/v1/processing/stats query of
// server/routes/processing.js: what the shield and the compressor did over the
// last 30 days.
func (d *deps) processingStats(ctx context.Context, userID string, sinceMillis int64) (processing.Stats, error) {
	stats := processing.Stats{Days: 30}
	row := d.db.R.QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(redacted_count > 0), SUM(redacted_count),
			SUM(compression_saved_tokens > 0), SUM(compression_saved_tokens)
		 FROM requests WHERE user_id = ? AND created_at >= ?`, userID, sinceMillis)
	var requests, protected, redacted, compressed, saved *int64
	if err := row.Scan(&requests, &protected, &redacted, &compressed, &saved); err != nil {
		return stats, err
	}
	stats.Requests = intOr(requests)
	stats.ProtectedRequests = intOr(protected)
	stats.Redacted = intOr(redacted)
	stats.CompressedRequests = intOr(compressed)
	stats.SavedTokens = intOr(saved)
	return stats, nil
}

func intOr(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}

// analyticsHandler wraps the static handler with server/analytics.js: the
// browser script and the script tag on the pages this server renders itself.
func (d *deps) analyticsHandler(next http.Handler) http.Handler {
	analytics := &status.Analytics{
		Getenv:     d.getenv,
		WebDir:     func() string { return d.cfg().WebDir },
		LandingDir: func() string { return d.cfg().Landing },
		Log:        func(err error) { d.log.Warn("analytics", "error", err) },
	}
	return analytics.Middleware(next)
}

// startHostAnalytics is host_analytics.js startHostAnalytics: one report now,
// then one an hour.
func (d *deps) startHostAnalytics(ctx context.Context) {
	host := status.Host{Version: d.version, StartedAt: d.startedAt}
	status.StartHostAnalytics(ctx, status.AnalyticsConfig(d.getenv), host, status.DefaultPoster(nil))
}

// startReporting runs the boot sequence of server/index.js: the routing
// initializer, the scheduler, the privacy filter and local models resuming, the
// pool relays, and the two samplers the public status page reads.
func (d *deps) startReporting(ctx context.Context) {
	if err := d.Routing.Initialize(ctx); err != nil {
		d.log.Warn("routing initialize", "error", err)
	}
	d.Scheduler.Start(ctx)
	d.Processing.Privacy.Resume(ctx)
	d.Local.Resume(ctx)
	d.Pools.StartRelays(ctx)

	history := d.Status.History
	history.Start(ctx, func(sampleCtx context.Context) (string, error) {
		return d.Status.Public(sampleCtx).Status, nil
	})
	d.startHostAnalytics(ctx)
}

var _ = time.Now
