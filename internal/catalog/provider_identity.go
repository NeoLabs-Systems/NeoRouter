package catalog

import (
	"regexp"
	"strings"
)

// ProviderIdentity is how one provider is keyed and named wherever models are
// shown or counted.
type ProviderIdentity struct {
	ProviderKey  string `json:"providerKey"`
	ProviderName string `json:"providerName"`
}

var customLocalDefinition = regexp.MustCompile(`^custom|^local`)

// providerIdentityOf groups the accounts of one provider into one provider:
// several accounts with one provider (two Groq keys) share a key and a name
// wherever models are shown or counted. Custom endpoints are one provider per
// address.
//
// Port of server/services/catalog/provider_identity.js.
func (s *Service) providerIdentityOf(definitionID, slug, label, baseURL string) ProviderIdentity {
	var def Definition
	known := false
	if s.Defs != nil {
		def, known = s.Defs.Definition(definitionID)
	}
	// A pool partner is named after the partner, one provider entry each.
	if known && def.Category == "pool" {
		name := label
		if name == "" {
			name = slug
		}
		return ProviderIdentity{ProviderKey: definitionID + ":" + slug, ProviderName: name}
	}
	if !known || customLocalDefinition.MatchString(definitionID) {
		address := baseURL
		if address == "" {
			address = slug
		}
		// A custom endpoint keeps the generic label it was created with
		// ("Custom OpenAI-compatible", "… 2"); its own name says more.
		name := slug
		if label != "" && (!known || !strings.HasPrefix(label, def.Label)) {
			name = label
		}
		return ProviderIdentity{ProviderKey: definitionID + ":" + address, ProviderName: name}
	}
	return ProviderIdentity{ProviderKey: definitionID, ProviderName: def.Label}
}
