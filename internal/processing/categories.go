package processing

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Categories and their placeholders, from shield/categories.json: what can be
// hidden, the token its placeholder is built from, the mode it starts with and
// how strongly it wins an overlap.
//
//go:embed shield/data/categories.json
var categoriesJSON []byte

// Category is one entry of categories.json.
type Category struct {
	ID string `json:"id"`
	// Token is the placeholder stem: the first hidden email address is
	// "[EMAIL_1]".
	Token string `json:"token"`
	// DefaultMode is "hide" (never put back) or "restore".
	DefaultMode string `json:"defaultMode"`
	// Priority decides overlaps: a lower number is more sensitive and wins.
	Priority int `json:"priority"`
	// AIOnly marks a category only the privacy filter can find.
	AIOnly bool `json:"aiOnly"`
}

var (
	categories []Category
	// Categories are the category ids, in the order categories.json lists them.
	Categories []string
	// DefaultModes is the starting mode per category.
	DefaultModes map[string]string
	// TokenTypes maps a category to its placeholder stem.
	TokenTypes map[string]string
	// Priorities is the overlap rank per category; a missing category ranks 5.
	Priorities map[string]int
)

func init() {
	if err := json.Unmarshal(categoriesJSON, &categories); err != nil {
		panic(fmt.Sprintf("processing: categories.json: %v", err))
	}
	Categories = make([]string, 0, len(categories))
	DefaultModes = make(map[string]string, len(categories))
	TokenTypes = make(map[string]string, len(categories))
	Priorities = make(map[string]int, len(categories))
	for _, category := range categories {
		Categories = append(Categories, category.ID)
		DefaultModes[category.ID] = category.DefaultMode
		TokenTypes[category.ID] = category.Token
		Priorities[category.ID] = category.Priority
	}
}

// CategoryList is categories.json, for the settings page (`aiOnly` marks the
// categories only the privacy filter can find).
func CategoryList() []Category { return categories }

// AIOnlyCategories are the ids of the categories only the AI pass finds.
func AIOnlyCategories() []string {
	ids := []string{}
	for _, category := range categories {
		if category.AIOnly {
			ids = append(ids, category.ID)
		}
	}
	return ids
}

// KnownCategory reports whether id is one of the categories.
func KnownCategory(id string) bool {
	_, ok := TokenTypes[id]
	return ok
}
