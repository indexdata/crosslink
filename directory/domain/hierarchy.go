package domain

func ValidParentForType(entryType, parentType string) (bool, string) {
	switch entryType {
	case "Institution":
		if parentType == "Consortium" {
			return true, ""
		}
		return false, "Institution parent must be of type Consortium"
	case "Branch":
		if parentType == "Institution" {
			return true, ""
		}
		return false, "Branch parent must be of type Institution"
	default:
		return false, "Invalid type to have parent"
	}
}
