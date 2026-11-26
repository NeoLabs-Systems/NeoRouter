package routing

// Shared pacing policy. Traffic shape, token size and plan consumption rates
// are measured; these only bound responsiveness and burst tolerance.
const (
	policyHistoryMs          = 14 * 86_400_000
	policyHistoryHalfLifeMs  = 7 * 86_400_000
	policyCacheMs            = 60_000
	policyProfileBlend       = 0.6
	policyPeakRoom           = 1.6
	policyMinHourShare       = 1.0 / 48
	policyDailyCushion       = 0.03
	policyPlanCushion        = 0.03
	policyPlanRateAlpha      = 0.2
	policySoftPressureStart  = 0.5
	policySoftPressureWeight = 2
	policyOutputPriorSamples = 10
	policyInflightRecheckMs  = 250
	policyRetrySearchMs      = 60_000
)
