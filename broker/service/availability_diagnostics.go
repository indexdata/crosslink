package service

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
)

// Keep event size bounded independently of the number of catalog holdings.
const availabilityCandidateLogLimit = 50
const availabilityDiagnosticTextLimit = 2048

type availabilityLookupDetails struct {
	SupplierSymbol      string `json:"supplierSymbol,omitempty"`
	BibliographicID     string `json:"bibliographicId,omitempty"`
	AttemptToken        string `json:"attemptToken,omitempty"`
	ConfigurationSource string `json:"configurationSource,omitempty"`
	Protocol            string `json:"protocol,omitempty"`
	Target              string `json:"target,omitempty"`
	Query               string `json:"query,omitempty"`
	DurationMS          int64  `json:"durationMs"`
	RecordFound         *bool  `json:"recordFound,omitempty"`
	AvailableHoldings   *int   `json:"availableHoldings,omitempty"`
	Outcome             string `json:"outcome"`
	ResultApplied       bool   `json:"resultApplied"`
}

func (d *availabilityLookupDetails) recordLookup(started time.Time, result catalog.LookupResult, err error) {
	d.DurationMS = time.Since(started).Milliseconds()
	d.Outcome = "lookup-failed"
	if result != nil {
		d.Query = diagnosticText(result.GetQuery())
		if err == nil {
			d.RecordFound = new(result.HasRecord())
		}
	}
}

func (d *availabilityLookupDetails) recordHoldings(started time.Time, holdings []catalog.Holding, err error) {
	d.DurationMS = time.Since(started).Milliseconds()
	d.Outcome = "holdings-failed"
	if err != nil {
		return
	}
	d.AvailableHoldings = new(len(holdings))
	switch {
	case d.RecordFound != nil && !*d.RecordFound:
		d.Outcome = "no-record"
	case len(holdings) == 0:
		d.Outcome = "no-available-holdings"
	default:
		d.Outcome = "available-holdings"
	}
}

type availabilityHoldingDetails struct {
	ItemID             string `json:"itemId"`
	Location           string `json:"location"`
	ShelvingLocation   string `json:"shelvingLocation"`
	ItemLoanPolicy     string `json:"itemLoanPolicy,omitempty"`
	LocationPreference int    `json:"locationPreference"`
	ShelvingPreference int    `json:"shelvingPreference"`
	Eligible           bool   `json:"eligible"`
	Reason             string `json:"reason"`
	Selected           bool   `json:"selected"`
}

type availabilityPolicyDetails struct {
	Status              string                       `json:"status"`
	Applied             bool                         `json:"applied"`
	CandidateCount      int                          `json:"candidateCount"`
	EligibleCount       int                          `json:"eligibleCount"`
	ExcludedCount       int                          `json:"excludedCount"`
	CandidatesTruncated int                          `json:"candidatesTruncated"`
	Candidates          []availabilityHoldingDetails `json:"candidates,omitempty"`
	Selected            *availabilityHoldingDetails  `json:"selected,omitempty"`
	SelectionReason     string                       `json:"selectionReason,omitempty"`
}

func newAvailabilityLookupDetails(supplier ill_db.LocatedSupplier) *availabilityLookupDetails {
	return &availabilityLookupDetails{
		SupplierSymbol:  diagnosticText(supplier.SupplierSymbol),
		BibliographicID: diagnosticText(supplier.LocalID.String),
		Outcome:         "not-started",
	}
}

func (d *availabilityLookupDetails) configure(entry dirapi.Entry) {
	effective, err := profiles.Resolve(entry)
	if err != nil || effective.Catalog == nil {
		return
	}
	if cfg := effective.Catalog.Sru; cfg != nil {
		d.Protocol, d.Target = "sru", diagnosticTarget(cfg.Address)
	} else if cfg := effective.Catalog.Zoom; cfg != nil {
		d.Protocol, d.Target = "z3950", diagnosticTarget(cfg.Address)
	}
}

func diagnosticText(value string) string {
	if len(value) <= availabilityDiagnosticTextLimit {
		return value
	}
	end := availabilityDiagnosticTextLimit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…"
}

func diagnosticTarget(address string) string {
	if !strings.Contains(address, "://") {
		address = "tcp://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return "[invalid target]"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	u.ForceQuery = false
	return diagnosticText(u.String())
}

func holdingDetails(supplier adapter.Supplier, eligible bool, reason string) availabilityHoldingDetails {
	if eligible {
		reason = "eligible"
	}
	return availabilityHoldingDetails{
		ItemID:             diagnosticText(supplier.HoldingItemID),
		Location:           diagnosticText(supplier.Location),
		ShelvingLocation:   diagnosticText(supplier.ShelvingLocation),
		ItemLoanPolicy:     diagnosticText(supplier.ItemLoanPolicy),
		LocationPreference: supplier.LocationPreference,
		ShelvingPreference: supplier.ShelvingPreference,
		Eligible:           eligible, Reason: reason,
	}
}

// evaluate records decisions while preserving catalog order and the existing comparator.
func (d *availabilityPolicyDetails) evaluate(entry dirapi.Entry, holdings []catalog.Holding) *adapter.Supplier {
	d.Status = "evaluated"
	d.CandidateCount = len(holdings)
	var selected, runnerUp *adapter.Supplier
	selectedIndex := -1
	for i, holding := range holdings {
		candidate := adapter.Supplier{CustomData: entry, HoldingItemID: holding.ItemId,
			Location: holding.Location, ShelvingLocation: holding.ShelvingLocation,
			ItemLoanPolicy: holding.ItemLoanPolicy}
		eligible, reason := adapter.EvaluateHoldingsPolicyWithReason(&candidate)
		if eligible {
			d.EligibleCount++
			if selected == nil || adapter.CompareSuppliers(candidate, *selected, dirapi.LoadBalancingPolicyProportional) < 0 {
				runnerUp, selected, selectedIndex = selected, &candidate, i
			} else if runnerUp == nil || adapter.CompareSuppliers(candidate, *runnerUp, dirapi.LoadBalancingPolicyProportional) < 0 {
				runnerUp = &candidate
			}
		} else {
			d.ExcludedCount++
		}
		if i < availabilityCandidateLogLimit {
			d.Candidates = append(d.Candidates, holdingDetails(candidate, eligible, reason))
		}
	}
	d.CandidatesTruncated = len(holdings) - len(d.Candidates)
	if selected == nil {
		d.SelectionReason = "all-holdings-excluded"
		if len(holdings) == 0 {
			d.SelectionReason = "no-available-holdings"
		}
		return nil
	}
	detail := holdingDetails(*selected, true, "")
	detail.Selected = true
	d.Selected = &detail
	if selectedIndex < len(d.Candidates) {
		d.Candidates[selectedIndex].Selected = true
	}
	switch {
	case runnerUp == nil:
		d.SelectionReason = "only-eligible-holding"
	case selected.LocationPreference > runnerUp.LocationPreference:
		d.SelectionReason = "higher-location-preference"
	case selected.ShelvingPreference > runnerUp.ShelvingPreference:
		d.SelectionReason = "higher-shelving-preference"
	default:
		d.SelectionReason = "equal-preference-first-returned"
	}
	return selected
}
