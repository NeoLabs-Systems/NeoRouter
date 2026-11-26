package catalog

// The facts layer: what a provider documents about a model whose listing says
// nothing about its limits (the definition's models.facts).

// ModelFacts are the limits a definition documents for one model. An absent
// field is a limit the definition does not document.
type ModelFacts struct {
	ContextWindow *int64
	MaxOutput     *int64
}

// FactsFor returns the limits the definition documents for one model; the zero
// value when it documents none.
func (d Definition) FactsFor(modelID string) ModelFacts {
	if d.ModelFacts == nil {
		return ModelFacts{}
	}
	return d.ModelFacts[modelID]
}

// mergeFacts layers the definition's documented limits over the models.dev
// facts of the same model, so the definition wins per field over models.dev.
// The live listing still wins over both, which the upsert applies with
// firstInt64.
func mergeFacts(base ProviderFacts, definition ModelFacts) ProviderFacts {
	if definition.ContextWindow != nil {
		base.ContextWindow = definition.ContextWindow
	}
	if definition.MaxOutput != nil {
		base.MaxOutput = definition.MaxOutput
	}
	return base
}
