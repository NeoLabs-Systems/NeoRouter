package apps

// Text is a title, step or summary in the languages the dashboard speaks.
type Text struct {
	EN string `json:"en"`
	DE string `json:"de"`
}

// Field is one setting an app asks for, in the app's own words.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"` // baseUrl, rootUrl, apiKey or model
}

// File is the config file an app reads, with a template for it.
type File struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Template string `json:"template"`
}

// App is one entry of the catalog: an app people connect to NeoRouter.
type App struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Group     string  `json:"group"` // coding, chat, automation or developer
	API       string  `json:"api"`   // openai or anthropic
	Summary   Text    `json:"summary"`
	Runs      string  `json:"runs"` // local, docker, network or internet
	CloudOnly bool    `json:"cloudOnly"`
	Steps     []Text  `json:"steps"`
	Fields    []Field `json:"fields"`
	File      *File   `json:"file,omitempty"`
	Website   string  `json:"website,omitempty"`
}

// All returns the app catalog in the order catalog.js lists it.
func All() []App { return catalog }

// Get returns one catalog entry by id.
func Get(id string) (App, bool) {
	for _, entry := range catalog {
		if entry.ID == id {
			return entry, true
		}
	}
	return App{}, false
}
