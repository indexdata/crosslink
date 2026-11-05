package service

import (
	"errors"
	"fmt"
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
	recordFound         *bool
	AvailableHoldings   *int   `json:"availableHoldings,omitempty"`
	Outcome             string `json:"outcome"`
	ResultApplied       bool   `json:"resultApplied"`
}

func (d *availabilityLookupDetails) recordLookup(started time.Time, result catalog.LookupResult, err error) {
	d.DurationMS = time.Since(started).Milliseconds()
	d.Outcome = "lookup-failed"
	if errors.Is(err, catalog.ErrHoldingsParse) {
		d.Outcome = "holdings-failed"
	}
	if result != nil {
		d.Query = diagnosticText(result.GetQuery())
		if err == nil {
			d.recordFound = new(result.HasRecord())
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
	case d.recordFound != nil && !*d.recordFound:
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
	Reason             string `json:"reason,omitempty"`
	Selected           bool   `json:"selected,omitempty"`
}

type availabilityPolicyDetails struct {
	Status              string                       `json:"status"`
	EligibleCount       int                          `json:"eligibleCount"`
	ExcludedCount       int                          `json:"excludedCount"`
	CandidatesTruncated int                          `json:"candidatesTruncated,omitempty"`
	Candidates          []availabilityHoldingDetails `json:"candidates,omitempty"`
	SelectionReason     string                       `json:"selectionReason,omitempty"`
	selected            *availabilityHoldingDetails
	runnerUp            *availabilityHoldingDetails
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
	if selectedIndex < len(d.Candidates) {
		d.Candidates[selectedIndex] = detail
	} else {
		// Reserve the last displayed slot for the winner, keeping the same cap.
		selectedIndex = len(d.Candidates) - 1
		d.Candidates[selectedIndex] = detail
	}
	d.selected = &d.Candidates[selectedIndex]
	if runnerUp != nil {
		other := holdingDetails(*runnerUp, true, "")
		d.runnerUp = &other
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

func itemCount(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// Discovery holdings can identify suppliers without identifying available items.
func (d *availabilityLookupDetails) discoverySummary() string {
	switch d.Outcome {
	case "available-holdings":
		return "Found " + itemCount(*d.AvailableHoldings, "holding") + "."
	case "no-available-holdings":
		return "Record found with no holdings returned."
	default:
		return d.summary()
	}
}

func (d *availabilityLookupDetails) summary() string {
	switch d.Outcome {
	case "available-holdings":
		return "Found " + itemCount(*d.AvailableHoldings, "available item") + "."
	case "no-record":
		return "No bibliographic record found."
	case "no-available-holdings":
		return "Record found with no available holdings."
	case "lookup-failed":
		return "Holdings lookup failed."
	case "holdings-failed":
		return "Could not parse holdings lookup results."
	case "not-configured":
		return "Holdings lookup is not configured."
	case "invalid-configuration", "peer-lookup-failed", "adapter-failed", "adapter-missing":
		return "Could not prepare the holdings lookup."
	case "transaction-read-failed":
		return "Could not read the request for holdings lookup."
	default:
		return "Holdings lookup was not started."
	}
}

func holdingName(holding *availabilityHoldingDetails) string {
	if holding.ShelvingLocation != "" {
		return holding.ShelvingLocation
	}
	if holding.Location != "" {
		return holding.Location
	}
	return "unspecified location"
}

func (d *availabilityPolicyDetails) summary() string {
	switch d.Status {
	case "not-configured":
		return "No holdings policy configured."
	case "applied-at-discovery":
		return "Holdings policy was applied during discovery."
	case "skipped-lookup-failure":
		return "Holdings policy skipped because the availability lookup failed."
	case "pending":
		return "Holdings policy remains unresolved."
	case "requires-discovery":
		return "Holdings policy requires a new discovery lookup."
	case "applied", "evaluated":
		if d.selected == nil {
			if d.ExcludedCount > 0 {
				verb := "excluded"
				if d.Status == "evaluated" {
					verb = "would exclude"
				}
				return "Holdings policy " + verb + " all " + itemCount(d.ExcludedCount, "available item") + "."
			}
			return "No available holdings to evaluate."
		}
	default:
		return ""
	}
	selected := d.selected
	copyName := selected.ItemID
	if copyName == "" {
		copyName = "an available holding"
	}
	verb := "selected"
	if d.Status == "evaluated" {
		verb = "would select"
	}
	text := "Holdings policy " + verb + " " + copyName + " at " + holdingName(selected)
	switch d.SelectionReason {
	case "higher-location-preference":
		location, otherLocation := selected.Location, d.runnerUp.Location
		if location == "" {
			location = "unspecified location"
		}
		if otherLocation == "" {
			otherLocation = "unspecified location"
		}
		return fmt.Sprintf("%s: location preference %d for %s, compared with %s's %d.", text,
			selected.LocationPreference, location, otherLocation, d.runnerUp.LocationPreference)
	case "higher-shelving-preference":
		return fmt.Sprintf("%s: shelving preference %d, compared with %s's %d.", text,
			selected.ShelvingPreference, holdingName(d.runnerUp), d.runnerUp.ShelvingPreference)
	case "only-eligible-holding":
		return text + ": it was the only eligible holding."
	default:
		return text + ": preferences tied, so the first returned eligible holding was retained."
	}
}

func centralPolicySummary(rota *adapter.RotaInfo) string {
	if rota == nil {
		return ""
	}
	counts := map[string]int{}
	excluded := 0
	for _, candidate := range rota.Suppliers {
		counts[candidate.HoldingsPolicyStatus]++
		if candidate.HoldingsPolicyEligible != nil && !*candidate.HoldingsPolicyEligible {
			excluded++
		}
	}
	var parts []string
	for _, status := range []string{"applied", "evaluated"} {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("Holdings policy %s for %s (%d excluded).", status, itemCount(n, "candidate"), excluded))
		}
	}
	if n := counts["deferred"]; n > 0 {
		parts = append(parts, "Holdings policy deferred to supplier availability for "+itemCount(n, "candidate")+".")
	}
	if n := counts["not-configured"]; n > 0 {
		parts = append(parts, "No holdings policy configured for "+itemCount(n, "candidate")+".")
	}
	return strings.Join(parts, " ")
}
