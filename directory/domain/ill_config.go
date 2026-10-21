package domain

// IllConfigCodeDescription pairs a configured ILL code with its default description.
// Codes may be supplied by legacy systems and are not restricted to default metadata.
type IllConfigCodeDescription struct {
	Code        string `json:"code"`
	DefaultDesc string `json:"defaultDesc"`
}

// IllConfigCodeDescriptions is the ordered list stored for an ILL configuration field.
type IllConfigCodeDescriptions = []IllConfigCodeDescription
