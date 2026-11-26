// Package discovery is NeoRouter's model discovery pages: the benchmark
// leaderboard, the free-model news feed, and the /api/v1/discovery endpoints
// behind them.
//
// It covers benchmarks, news and the refresh policy. The measured results come
// from the catalog domain's metadata projection, so this package declares the
// narrow Catalog interface it needs and internal/app adapts the catalog to it.
package discovery

// Policy is the refresh policy.
const (
	// PageSize is the default page length of both endpoints.
	PageSize = 24
	// MaxPageSize is the largest page a caller may ask for.
	MaxPageSize = 50
	// NewsQuantile is the score percentile a free model must reach to be news.
	NewsQuantile = 0.5
)
