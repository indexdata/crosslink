package iso18626

import "fmt"

type ReasonRetry string

// for now, a tiny subset of https://illtransactions.org/opencode/2017/
const (
	ReasonRetryCostExceedsMaxCost ReasonRetry = "CostExceedsMaxCost"
	ReasonRetryOnLoan             ReasonRetry = "OnLoan"
	ReasonRetryLoanCondition      ReasonRetry = "LoanCondition"
)

type SentVia string

const (
	SentViaMail  SentVia = "Mail"
	SentViaEmail SentVia = "Email"
	SentViaFtp   SentVia = "FTP"
	SentViaUrl   SentVia = "URL"
)

type ElectronicAddressType string

const (
	ElectronicAddressTypeEmail ElectronicAddressType = "Email"
	ElectronicAddressTypeFtp   ElectronicAddressType = "FTP"
)

type Format string

const (
	FormatPdf       Format = "PDF"
	FormatPrinted   Format = "Printed"
	FormatPaperCopy Format = "PaperCopy"
)

type BibliographicItemIdCode string

const (
	BibliographicItemIdCodeISBN BibliographicItemIdCode = "ISBN"
	BibliographicItemIdCodeISSN BibliographicItemIdCode = "ISSN"
	BibliographicItemIdCodeISMN BibliographicItemIdCode = "ISMN"
)

func BibliographicItemIdCodeFromString(s string) (BibliographicItemIdCode, error) {
	switch BibliographicItemIdCode(s) {
	case BibliographicItemIdCodeISBN,
		BibliographicItemIdCodeISSN,
		BibliographicItemIdCodeISMN:
		return BibliographicItemIdCode(s), nil
	default:
		return "", fmt.Errorf("invalid BibliographicItemIdCode: %s", s)
	}
}

type BibliographicRecordIdCode string

const (
	BibliographicRecordIdCodeOCLC    BibliographicRecordIdCode = "OCLC"
	BibliographicRecordIdCodeLCCN    BibliographicRecordIdCode = "LCCN"
	BibliographicRecordIdCodeAMICUS  BibliographicRecordIdCode = "AMICUS"
	BibliographicRecordIdCodeBL      BibliographicRecordIdCode = "BL"
	BibliographicRecordIdCodeFAUST   BibliographicRecordIdCode = "FAUST"
	BibliographicRecordIdCodeJNB     BibliographicRecordIdCode = "JNB"
	BibliographicRecordIdCodeLA      BibliographicRecordIdCode = "LA"
	BibliographicRecordIdCodeMedline BibliographicRecordIdCode = "Medline"
	BibliographicRecordIdCodeNCID    BibliographicRecordIdCode = "NCID"
	BibliographicRecordIdCodePID     BibliographicRecordIdCode = "PID"
	BibliographicRecordIdCodePMID    BibliographicRecordIdCode = "PMID"
	BibliographicRecordIdCodeTP      BibliographicRecordIdCode = "TP"
)

func BibliographicRecordIdCodeFromString(s string) (BibliographicRecordIdCode, error) {
	switch BibliographicRecordIdCode(s) {
	case BibliographicRecordIdCodeOCLC,
		BibliographicRecordIdCodeLCCN,
		BibliographicRecordIdCodeAMICUS,
		BibliographicRecordIdCodeBL,
		BibliographicRecordIdCodeFAUST,
		BibliographicRecordIdCodeJNB,
		BibliographicRecordIdCodeLA,
		BibliographicRecordIdCodeMedline,
		BibliographicRecordIdCodeNCID,
		BibliographicRecordIdCodePID,
		BibliographicRecordIdCodePMID,
		BibliographicRecordIdCodeTP:
		return BibliographicRecordIdCode(s), nil
	default:
		return "", fmt.Errorf("invalid BibliographicRecordIdCode: %s", s)
	}
}

type ServiceLevel string

const (
	ServiceLevelExpress       ServiceLevel = "Express"
	ServiceLevelNormal        ServiceLevel = "Normal"
	ServiceLevelRush          ServiceLevel = "Rush"
	ServiceLevelSecondaryMail ServiceLevel = "SecondaryMail"
	ServiceLevelStandard      ServiceLevel = "Standard"
	ServiceLevelUrgent        ServiceLevel = "Urgent"
)

type PublicationType string

const (
	PublicationTypeArchiveMaterial PublicationType = "ArchiveMaterial"
	PublicationTypeArticle         PublicationType = "Article"
	PublicationTypeAudioBook       PublicationType = "AudioBook"
	PublicationTypeBook            PublicationType = "Book"
	PublicationTypeChapter         PublicationType = "Chapter"
	PublicationTypeConferenceProc  PublicationType = "ConferenceProc"
	PublicationTypeGame            PublicationType = "Game"
	PublicationTypeGovernmentPubl  PublicationType = "GovernmentPubl"
	PublicationTypeImage           PublicationType = "Image"
	PublicationTypeJournal         PublicationType = "Journal"
	PublicationTypeManuscript      PublicationType = "Manuscript"
	PublicationTypeMap             PublicationType = "Map"
	PublicationTypeMovie           PublicationType = "Movie"
	PublicationTypeMusicRecording  PublicationType = "MusicRecording"
	PublicationTypeMusicScore      PublicationType = "MusicScore"
	PublicationTypeNewspaper       PublicationType = "Newspaper"
	PublicationTypePatent          PublicationType = "Patent"
	PublicationTypeReport          PublicationType = "Report"
	PublicationTypeSoundRecording  PublicationType = "SoundRecording"
	PublicationTypeThesis          PublicationType = "Thesis"
)

type LoanCondition string

const (
	LoanConditionLibraryUseOnly      LoanCondition = "LibraryUseOnly"
	LoanConditionNoReproduction      LoanCondition = "NoReproduction"
	LoanConditionSignatureRequired   LoanCondition = "SignatureRequired"
	LoanConditionSpecCollSupervReq   LoanCondition = "SpecCollSupervReq"
	LoanConditionWatchLibraryUseOnly LoanCondition = "WatchLibraryUseOnly"
)
