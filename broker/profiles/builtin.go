package profiles

type builtinProfile struct {
	LMS     object `json:"lmsConfig"`
	Catalog object `json:"catalogConfig"`
}
