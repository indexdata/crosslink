package prservice

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5/pgtype"
)

//go:embed templatesamples/*.json
var templateSampleFiles embed.FS

// TemplateSampleNames lists the built-in samples in the order previews render
// them. Each name is a file in templatesamples.
var TemplateSampleNames = []string{"typical", "long", "sparse", "requester"}

// TemplateSample is a patron request that template previews render against:
// a built-in one, which has a Name and is never stored, or a stored one, which
// has its RequestID instead.
type TemplateSample struct {
	Name       string
	RequestID  string
	Request    pr_db.PatronRequest
	Notes      []pr_db.Notification
	Conditions []pr_db.Notification
}

// templateSampleFile is the JSON shape of a file in templatesamples. The
// request uses the Go field names of pr_db.PatronRequest, and notes and
// conditions are the text of each notification.
type templateSampleFile struct {
	Request    pr_db.PatronRequest `json:"request"`
	Notes      []string            `json:"notes"`
	Conditions []string            `json:"conditions"`
}

// Each call to GetTemplateSamples returns fresh copies that the caller may modify.
func GetTemplateSamples() ([]TemplateSample, error) {
	samples := make([]TemplateSample, 0, len(TemplateSampleNames))
	for _, name := range TemplateSampleNames {
		sample, err := getTemplateSample(name)
		if err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func getTemplateSample(name string) (TemplateSample, error) {
	raw, err := templateSampleFiles.ReadFile("templatesamples/" + name + ".json")
	if err != nil {
		return TemplateSample{}, fmt.Errorf("read template sample %q: %w", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var file templateSampleFile
	if err := decoder.Decode(&file); err != nil {
		return TemplateSample{}, fmt.Errorf("decode template sample %q: %w", name, err)
	}
	sample := TemplateSample{Name: name, Request: file.Request}
	for _, note := range file.Notes {
		sample.Notes = append(sample.Notes, pr_db.Notification{
			PrID: file.Request.ID,
			Kind: pr_db.NotificationKindNote,
			Note: pgtype.Text{String: note, Valid: true},
		})
	}
	for _, condition := range file.Conditions {
		sample.Conditions = append(sample.Conditions, pr_db.Notification{
			PrID:      file.Request.ID,
			Kind:      pr_db.NotificationKindCondition,
			Condition: pgtype.Text{String: condition, Valid: true},
		})
	}
	return sample, nil
}
