package ncip

const NCIP_V2_02_XSD = "http://www.niso.org/ncip/v2_02/imp1/xsd/ncip_v2_02.xsd"

type ProblemTypeMessage string

// Just a few from Appendix A of NCIP 2.02
const (
	MissingVersion            ProblemTypeMessage = "Missing Version"
	UnsupportedService        ProblemTypeMessage = "Unsupported Service"
	NeededDataMissing         ProblemTypeMessage = "Needed Data Missing"
	InvalidMessageSyntaxError ProblemTypeMessage = "Invalid Message Syntax Error"
	UnknownUser               ProblemTypeMessage = "Unknown User"
	UnknownItem               ProblemTypeMessage = "Unknown Item"
)
