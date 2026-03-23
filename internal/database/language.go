package database

// Language represents a supported sermon language.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// supportedLanguages maps language code to the ID stored in the books collection.
var supportedLanguages = map[string]int{
	"en": 1,
	"de": 2,
}

var availableLanguages = []Language{
	{Code: "en", Name: "English", ID: 1},
	{Code: "de", Name: "Deutsch", ID: 2},
}

func (d *Database) GetLanguages() []Language {
	return availableLanguages
}
