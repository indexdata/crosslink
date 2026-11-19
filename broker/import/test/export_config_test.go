package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	"github.com/indexdata/crosslink/broker/import/service"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/teambition/rrule-go"
)

type configExportRecord struct {
	Type string `json:"type"`
	Data struct {
		Title        string         `json:"title"`
		Labels       []string       `json:"labels"`
		Audience     string         `json:"audience"`
		Schedule     string         `json:"schedule"`
		ActionName   string         `json:"actionName"`
		ActionParams map[string]any `json:"actionParams"`
	} `json:"data"`
	raw json.RawMessage
}

func configExportTx(t *testing.T) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := importTestPool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
	// Only the legacy columns read by the actual configuration exporter are
	// needed. UUID columns match mod-rs and catch accidental UUID/text joins.
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE app_setting (st_key text, st_value text, st_default_value text) ON COMMIT DROP;
		CREATE TEMP TABLE template_container (tmc_id uuid, tmc_name text) ON COMMIT DROP;
		CREATE TEMP TABLE localized_template (ltm_id uuid, ltm_owner_fk uuid, ltm_template_fk uuid, ltm_locality text) ON COMMIT DROP;
		CREATE TEMP TABLE template (tm_id uuid, tm_header text, tm_template_body text) ON COMMIT DROP;
		CREATE TEMP TABLE notice_policy_notice (npn_template_fk uuid, npn_trigger_fk uuid) ON COMMIT DROP;
		CREATE TEMP TABLE refdata_value (rdv_id uuid, rdv_value text) ON COMMIT DROP;
		CREATE TEMP TABLE timer (tr_id uuid, tr_code text, tr_description text, tr_task_code text, tr_rrule text, tr_task_config text) ON COMMIT DROP;
	`)
	require.NoError(t, err)
	return tx
}

func configTemplateID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

func insertConfigTemplate(t *testing.T, tx pgx.Tx, n int, title, body, trigger string) {
	t.Helper()
	ctx := context.Background()
	id := configTemplateID(n)
	_, err := tx.Exec(ctx, `INSERT INTO template_container VALUES ($1, $2)`, id, title)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO template VALUES ($1, 'Notice', $2)`, id, body)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO localized_template VALUES ($1, $1, $1, 'en')`, id)
	require.NoError(t, err)
	if trigger != "" {
		_, err = tx.Exec(ctx, `INSERT INTO refdata_value VALUES ($1, $2)`, id, trigger)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO notice_policy_notice VALUES ($1, $1)`, id)
		require.NoError(t, err)
	}
}

func executeConfigExport(t *testing.T, tx pgx.Tx, owner string) error {
	t.Helper()
	source, err := os.ReadFile("../../../migration/export-crosslink-config.sql")
	require.NoError(t, err)
	_, query, found := strings.Cut(string(source), "BEGIN;\n")
	require.True(t, found)
	query, _, found = strings.Cut(query, "-- Keep this boundary")
	require.True(t, found)
	// Execute the export's functions and complete projection. Leave psql's file
	// output and transaction management to the script itself.
	ctx := context.Background()
	_, err = tx.Exec(ctx, `SELECT set_config('crosslink.test_owner', $1, true)`, owner)
	require.NoError(t, err)
	query = strings.ReplaceAll(query, ":'owner'", "current_setting('crosslink.test_owner')")
	_, err = tx.Exec(ctx, query)
	return err
}

func exportConfigRecords(t *testing.T, tx pgx.Tx, owner string) []configExportRecord {
	t.Helper()
	require.NoError(t, executeConfigExport(t, tx, owner))
	rows, err := tx.Query(context.Background(), `SELECT record FROM crosslink_config_records ORDER BY record_type_order, record_order`)
	require.NoError(t, err)
	defer rows.Close()
	var records []configExportRecord
	for rows.Next() {
		var record configExportRecord
		require.NoError(t, rows.Scan(&record.raw))
		require.NoError(t, json.Unmarshal(record.raw, &record))
		records = append(records, record)
	}
	require.NoError(t, rows.Err())
	return records
}

func TestExportConfigNoticeTemplates(t *testing.T) {
	tx := configExportTx(t)
	for i, title := range []string{
		"New host LMS location", " New host LMS shelving location ", "NEW PATRON PROFILE",
	} {
		// Exclude unsupported notices even when their content would otherwise be
		// compatible, and do not put them in the template review output either.
		insertConfigTemplate(t, tx, i+1, title, "New {{item.location}}", "")
	}
	for _, tc := range []struct {
		n       int
		title   string
		trigger string
	}{
		{10, "Renamed processing notice", "new_request"},
		{11, "Article/Book Chapter available", "document_delivered"},
		{12, "Request unfilled", "end_of_rota"},
		{13, "Request canceled", "request_cancelled"},
		{14, "Request canceled - exceeds limit", "over_limit"},
		{15, "Supply notice", "new_supply_request"},
	} {
		insertConfigTemplate(t, tx, tc.n, tc.title, "Request {{request.id}}", tc.trigger)
	}
	insertConfigTemplate(t, tx, 20, "Alternate digital notice", "Request {{request.id}}", "Loaned digitally")
	insertConfigTemplate(t, tx, 21, "Reviewed unfilled notice", "Request {{request.id}}", "End of rota reviewed")
	insertConfigTemplate(t, tx, 22, "Expedited supply notice", "Request {{request.id}}", "new_supply_request_expedited")
	insertConfigTemplate(t, tx, 23, "Request cancelled", "Request {{request.id}}", "request_cancelled")
	insertConfigTemplate(t, tx, 24, "Unconfigured custom notice", "Request {{request.id}}", "")
	insertConfigTemplate(t, tx, 4, "Unsupported cancellation syntax", "{{unknown.path}}", "request_cancelled")
	insertConfigTemplate(t, tx, 5, "Batch-only cancellation", "{{numRequests}}", "request_cancelled")
	insertConfigTemplate(t, tx, 6, "Selected pull slips", "Pull slips", "request_cancelled")
	_, err := tx.Exec(context.Background(), `
		INSERT INTO app_setting VALUES ('pull_slip_template_id', $1, NULL);
	`, configTemplateID(6))
	require.NoError(t, err)
	// Multiple English children and repeated policy references must not produce
	// overlapping notification labels or duplicate labels within a template.
	_, err = tx.Exec(context.Background(), `
		INSERT INTO localized_template VALUES ($1, $2, $2, 'EN');
	`, configTemplateID(30), configTemplateID(13))
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), `INSERT INTO notice_policy_notice VALUES ($1, $1)`, configTemplateID(13))
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), `INSERT INTO timer VALUES
		($1, 'slips', 'Pull slips', 'PrintPullSlips', 'FREQ=DAILY',
		 '{"emailAddresses":["staff@example.test"],"attachPullSlips":true}')`, uuid.New())
	require.NoError(t, err)

	owner := "ISIL:" + uuid.NewString()
	insertPeer(owner, uuid.NewString())
	records := exportConfigRecords(t, tx, owner)
	var ndjson bytes.Buffer
	labels := map[string]string{
		"Renamed processing notice":        "new-request-notification",
		"Article/Book Chapter available":   "copy-completed-notification",
		"Request unfilled":                 "unfilled-notification",
		"Request canceled":                 "cancelled-notification",
		"Request canceled - exceeds limit": "over-limit-notification",
		"Supply notice":                    "new-supply-request-notification",
	}
	seen := make(map[string]bool)
	templates := 0
	for _, record := range records {
		require.NotContains(t, strings.ToLower(record.Data.Title), "new host lms")
		require.NotContains(t, strings.ToLower(record.Data.Title), "new patron profile")
		if record.Type == "unsupportedTemplate" {
			require.Equal(t, "Unsupported cancellation syntax", record.Data.Title)
			continue
		}
		if record.Type == "batchAction" {
			require.Equal(t, "email-pullslips", record.Data.ActionName)
			require.Equal(t, "mod-rs-template-"+configTemplateID(6), record.Data.ActionParams["templateLabel"])
			_, err = ndjson.Write(append(record.raw, '\n'))
			require.NoError(t, err)
			continue
		}
		require.Equal(t, "template", record.Type)
		templates++
		if label, ok := labels[record.Data.Title]; ok && !seen[label] {
			require.Len(t, record.Data.Labels, 2)
			require.Equal(t, label, record.Data.Labels[1])
			audience := "patron"
			if label == "new-supply-request-notification" {
				audience = "staff"
			}
			require.Equal(t, audience, record.Data.Audience)
			seen[label] = true
		} else {
			require.Len(t, record.Data.Labels, 1, record.Data.Title)
		}
		_, err = ndjson.Write(append(record.raw, '\n'))
		require.NoError(t, err)
	}
	require.Len(t, seen, len(labels))
	require.Len(t, records, 16)
	importer := service.NewImporter(importTestRepo, ill_db.CreateIllRepo(importTestPool), nil, &prservice.StateModelService{}, stubPrefixResolver{}, time.Now)
	result, err := importer.Import(importTestCtx, importdb.ConflictPolicyFail, &ndjson)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.EqualValues(t, templates, result.Templates.Imported)
	require.EqualValues(t, 1, result.BatchActions.Imported)
}

func TestExportConfigNoticeTriggerAliases(t *testing.T) {
	for _, tc := range []struct {
		trigger string
		label   string
	}{
		{"Loaned digitally", "copy-completed-notification"},
		{"End of rota reviewed", "unfilled-notification"},
		{"new_supply_request_expedited", "new-supply-request-notification"},
	} {
		t.Run(tc.trigger, func(t *testing.T) {
			tx := configExportTx(t)
			insertConfigTemplate(t, tx, 1, "Custom notice", "Request {{request.id}}", tc.trigger)
			records := exportConfigRecords(t, tx, "ISIL:CONFIG-TEST")
			require.Len(t, records, 1)
			require.Equal(t, []string{"mod-rs-template-" + configTemplateID(1), tc.label}, records[0].Data.Labels)
		})
	}
}

func TestExportConfigSharedNoticeAudience(t *testing.T) {
	tx := configExportTx(t)
	insertConfigTemplate(t, tx, 1, "Shared notice", "Request {{request.id}}", "new_request")
	_, err := tx.Exec(context.Background(), `INSERT INTO refdata_value VALUES ($1, 'new_supply_request')`, configTemplateID(2))
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), `INSERT INTO notice_policy_notice VALUES ($1, $2)`, configTemplateID(1), configTemplateID(2))
	require.NoError(t, err)
	records := exportConfigRecords(t, tx, "ISIL:CONFIG-TEST")
	require.Len(t, records, 1)
	require.Equal(t, []string{"mod-rs-template-" + configTemplateID(1), "new-request-notification", "new-supply-request-notification"}, records[0].Data.Labels)
	require.Empty(t, records[0].Data.Audience)
}

func TestExportConfigAgingWeekends(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    *string
		fallback *string
		schedule string
		want     string
	}{
		{"yes", new("yes"), nil, "FREQ=DAILY;BYHOUR=6;BYMINUTE=30", "FREQ=WEEKLY;BYHOUR=6;BYMINUTE=30;BYDAY=MO,TU,WE,TH,FR"},
		{"no", new("no"), new("yes"), "FREQ=DAILY;BYHOUR=6;BYMINUTE=30", "FREQ=DAILY;BYHOUR=6;BYMINUTE=30"},
		{"blank uses yes default", new(" "), new(" YES "), "FREQ=DAILY", "FREQ=WEEKLY;BYHOUR=0;BYMINUTE=0;BYDAY=MO,TU,WE,TH,FR"},
		{"null uses no default", nil, new("no"), "FREQ=DAILY", "FREQ=DAILY"},
		{"missing setting defaults yes", nil, nil, "FREQ=DAILY", "FREQ=WEEKLY;BYHOUR=0;BYMINUTE=0;BYDAY=MO,TU,WE,TH,FR"},
		{"existing days intersected", new("Yes"), nil, "RRULE:FREQ=WEEKLY;BYDAY=SU,MO,TU,WE,TH,FR,SA;BYHOUR=9;BYMINUTE=15", "FREQ=WEEKLY;BYHOUR=9;BYMINUTE=15;BYDAY=MO,TU,WE,TH,FR"},
		{"weekly Monday preserved", new("yes"), nil, "FREQ=WEEKLY;BYDAY=MO", "FREQ=WEEKLY;BYHOUR=0;BYMINUTE=0;BYDAY=MO"},
		{"weekly mixed days intersected", new("yes"), nil, "FREQ=WEEKLY;INTERVAL=2;BYDAY=FR,SA,MO,SU;BYHOUR=9;BYMINUTE=15", "FREQ=WEEKLY;INTERVAL=2;BYHOUR=9;BYMINUTE=15;BYDAY=FR,MO"},
		{"monthly first Monday preserved", new("yes"), nil, "FREQ=MONTHLY;BYDAY=1MO", "FREQ=MONTHLY;BYDAY=1MO"},
		{"monthly last Friday preserved", new("yes"), nil, "FREQ=MONTHLY;BYDAY=-1FR", "FREQ=MONTHLY;BYDAY=-1FR"},
		{"monthly mixed ordinals intersected", new("yes"), nil, "FREQ=MONTHLY;BYDAY=1MO,1SA,-1FR,-1SU;BYHOUR=6", "FREQ=MONTHLY;BYHOUR=6;BYDAY=1MO,-1FR"},
		{"monthly explicit dates preserved", new("yes"), nil, "FREQ=MONTHLY;BYMONTHDAY=13,28", "FREQ=MONTHLY;BYMONTHDAY=13,28;BYDAY=MO,TU,WE,TH,FR"},
		{"yearly explicit dates preserved", new("yes"), nil, "FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=13,28", "FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=13,28;BYDAY=MO,TU,WE,TH,FR"},
		{"yearly year days preserved", new("yes"), nil, "FREQ=YEARLY;BYYEARDAY=1,100", "FREQ=YEARLY;BYYEARDAY=1,100;BYDAY=MO,TU,WE,TH,FR"},
		{"yearly week numbers preserved", new("yes"), nil, "FREQ=YEARLY;BYWEEKNO=1", "FREQ=YEARLY;BYWEEKNO=1;BYDAY=MO,TU,WE,TH,FR"},
		{"yearly Easter offset preserved", new("yes"), nil, "FREQ=YEARLY;BYEASTER=1", "FREQ=YEARLY;BYEASTER=1;BYDAY=MO,TU,WE,TH,FR"},
		{"implicit weekly without exclusion", new("no"), nil, "FREQ=WEEKLY", "FREQ=WEEKLY"},
		{"implicit monthly without exclusion", new("no"), nil, "FREQ=MONTHLY", "FREQ=MONTHLY"},
		{"implicit yearly without exclusion", new("no"), nil, "FREQ=YEARLY", "FREQ=YEARLY"},
		{"weekend only without exclusion", new("no"), nil, "FREQ=WEEKLY;BYDAY=SA,SU", "FREQ=WEEKLY;BYDAY=SA,SU"},
		{"daily unit interval", new("yes"), nil, "FREQ=DAILY;INTERVAL=1;BYHOUR=5;BYMINUTE=0", "FREQ=WEEKLY;BYHOUR=5;BYMINUTE=0;BYDAY=MO,TU,WE,TH,FR"},
		{"weekday count preserved", new("yes"), nil, "FREQ=DAILY;COUNT=3;BYDAY=MO,FR", "FREQ=WEEKLY;COUNT=3;BYHOUR=0;BYMINUTE=0;BYDAY=MO,FR"},
		{"weekday positional count preserved", new("yes"), nil, "FREQ=DAILY;COUNT=3;BYDAY=MO,FR;BYHOUR=6;BYSETPOS=1", "FREQ=DAILY;COUNT=3;BYHOUR=6;BYSETPOS=1;BYDAY=MO,FR"},
		{"monthly ordinal count preserved", new("yes"), nil, "FREQ=MONTHLY;COUNT=3;BYDAY=1MO", "FREQ=MONTHLY;COUNT=3;BYDAY=1MO"},
		{"zero count is unbounded", new("yes"), nil, "FREQ=DAILY;COUNT=0", "FREQ=WEEKLY;COUNT=0;BYHOUR=0;BYMINUTE=0;BYDAY=MO,TU,WE,TH,FR"},
		{"count without exclusion", new("no"), nil, "FREQ=DAILY;COUNT=3", "FREQ=DAILY;COUNT=3"},
		{"daily first position preserves period", new("yes"), nil, "FREQ=DAILY;BYHOUR=6;BYSETPOS=1", "FREQ=DAILY;BYHOUR=6;BYSETPOS=1;BYDAY=MO,TU,WE,TH,FR"},
		{"daily last position preserves period", new("yes"), nil, "FREQ=DAILY;INTERVAL=1;BYHOUR=6,18;BYSETPOS=-1", "FREQ=DAILY;INTERVAL=1;BYHOUR=6,18;BYSETPOS=-1;BYDAY=MO,TU,WE,TH,FR"},
		{"daily positional days intersected", new("yes"), nil, "FREQ=DAILY;BYDAY=MO,FR,SA;BYHOUR=6,18;BYSETPOS=1,-1", "FREQ=DAILY;BYHOUR=6,18;BYSETPOS=1,-1;BYDAY=MO,FR"},
		{"weekly weekday positions preserved", new("yes"), nil, "FREQ=WEEKLY;BYDAY=MO,FR;BYHOUR=6;BYMINUTE=0;BYSETPOS=-1", "FREQ=WEEKLY;BYHOUR=6;BYMINUTE=0;BYSETPOS=-1;BYDAY=MO,FR"},
		{"monthly weekday positions preserved", new("yes"), nil, "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1", "FREQ=MONTHLY;BYSETPOS=-1;BYDAY=MO,TU,WE,TH,FR"},
		{"yearly weekday positions preserved", new("yes"), nil, "FREQ=YEARLY;BYDAY=MO,FR;BYSETPOS=1,-1", "FREQ=YEARLY;BYSETPOS=1,-1;BYDAY=MO,FR"},
		{"monthly positions without exclusion", new("no"), nil, "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR,SA,SU;BYSETPOS=-1", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR,SA,SU;BYSETPOS=-1"},
		{"daily non-unit interval preserved", new("yes"), nil, "FREQ=DAILY;INTERVAL=2;BYHOUR=5", "FREQ=DAILY;INTERVAL=2;BYHOUR=5;BYDAY=MO,TU,WE,TH,FR"},
		{"hourly cadence preserved", new("yes"), nil, "FREQ=HOURLY;INTERVAL=2;BYMINUTE=15", "FREQ=HOURLY;INTERVAL=2;BYMINUTE=15;BYDAY=MO,TU,WE,TH,FR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := configExportTx(t)
			ctx := context.Background()
			excludeWeekends := true
			for _, setting := range []*string{tc.value, tc.fallback} {
				if setting != nil && strings.TrimSpace(*setting) != "" {
					excludeWeekends = strings.EqualFold(strings.TrimSpace(*setting), "yes")
					break
				}
			}
			if tc.value != nil || tc.fallback != nil {
				_, err := tx.Exec(ctx, `INSERT INTO app_setting VALUES ('stale_request_3_exclude_weekend', $1, $2)`, tc.value, tc.fallback)
				require.NoError(t, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO app_setting VALUES
				('stale_request_2_days', '', '3'),
				('stale_request_rush_hours', '24', NULL),
				('stale_request_express_hours', '2', NULL)`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO timer VALUES ($1, 'aging', 'Aging', 'CheckForStaleSupplierRequests', $2, NULL)`, uuid.New(), tc.schedule)
			require.NoError(t, err)
			records := exportConfigRecords(t, tx, "ISIL:CONFIG-TEST")
			require.Len(t, records, 3)
			for _, record := range records {
				require.Equal(t, "request-aging", record.Data.ActionName)
				want, interval := strings.TrimPrefix(tc.schedule, "RRULE:"), "24h"
				switch record.Data.Title {
				case "Request aging of default requests":
					want, interval = tc.want, "72h"
				case "Request aging of express requests":
					interval = "2h"
				}
				require.Equal(t, want, record.Data.Schedule)
				require.Equal(t, interval, record.Data.ActionParams["interval"])
				// Check both weekday and weekend anchors, including implicit dates
				// when exclusion is disabled and explicit dates without BYDAY.
				for _, day := range []int{13, 14} { // Friday, Saturday
					options, err := rrule.StrToROption(record.Data.Schedule)
					require.NoError(t, err)
					options.Dtstart = time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC)
					rule, err := rrule.NewRRule(*options)
					require.NoError(t, err)
					original, err := rrule.StrToROption(strings.TrimPrefix(tc.schedule, "RRULE:"))
					require.NoError(t, err)
					original.Dtstart = options.Dtstart
					originalRule, err := rrule.NewRRule(*original)
					require.NoError(t, err)
					end := options.Dtstart.AddDate(0, 6, 0)
					wantRuns := originalRule.Between(options.Dtstart, end, true)
					if record.Data.Title == "Request aging of default requests" && excludeWeekends {
						var weekdays []time.Time
						for _, run := range wantRuns {
							if run.Weekday() != time.Saturday && run.Weekday() != time.Sunday {
								weekdays = append(weekdays, run)
							}
						}
						wantRuns = weekdays
					}
					runs := rule.Between(options.Dtstart, end, true)
					require.NotEmpty(t, runs)
					require.Equal(t, wantRuns, runs, "anchor %s", options.Dtstart)
				}
			}
		})
	}
}

func requireAgingScheduleRejected(t *testing.T, schedule, message string) {
	t.Helper()
	tx := configExportTx(t)
	_, err := tx.Exec(context.Background(), `INSERT INTO timer VALUES ($1, 'aging', 'Aging', 'CheckForStaleSupplierRequests', $2, NULL)`, uuid.New(), schedule)
	require.NoError(t, err)

	err = executeConfigExport(t, tx, "ISIL:CONFIG-TEST")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "22023", pgErr.Code)
	require.Contains(t, pgErr.Message, message)
	require.Contains(t, pgErr.Message, schedule)
	require.Contains(t, pgErr.Hint, "stale_request_3_exclude_weekend")
}

func TestExportConfigAgingImplicitDatesRejected(t *testing.T) {
	for _, schedule := range []string{
		"FREQ=WEEKLY",
		"FREQ=WEEKLY;INTERVAL=2;BYHOUR=6",
		"FREQ=MONTHLY",
		"FREQ=MONTHLY;BYMONTH=12;BYHOUR=6",
		"FREQ=YEARLY",
		"FREQ=YEARLY;BYMONTH=12;BYMINUTE=30",
	} {
		t.Run(schedule, func(t *testing.T) {
			requireAgingScheduleRejected(t, schedule, "Weekend exclusion would replace implicit anchor dates")
		})
	}
}

func TestExportConfigAgingCountChangesRejected(t *testing.T) {
	for _, schedule := range []string{
		"FREQ=DAILY;COUNT=3",
		"FREQ=DAILY;COUNT=+03",
		"FREQ=DAILY;COUNT=0003",
		"FREQ=DAILY;COUNT=3;BYDAY=FR,SA,SU",
		"FREQ=HOURLY;COUNT=3",
		"FREQ=MONTHLY;COUNT=3;BYMONTHDAY=13",
		"FREQ=YEARLY;COUNT=3;BYDAY=MO,SA",
	} {
		t.Run(schedule, func(t *testing.T) {
			requireAgingScheduleRejected(t, schedule, "Weekend exclusion would change COUNT-limited occurrences")
		})
	}
}

func TestExportConfigAgingWeekendOnlyRejected(t *testing.T) {
	for _, schedule := range []string{
		"FREQ=WEEKLY;BYDAY=SA,SU",
		"FREQ=DAILY;BYDAY=SA",
		"FREQ=MONTHLY;BYDAY=1SA,-1SU",
	} {
		t.Run(schedule, func(t *testing.T) {
			requireAgingScheduleRejected(t, schedule, "Weekend exclusion leaves no weekdays")
		})
	}
}

func TestExportConfigAgingPositionalDayChangesRejected(t *testing.T) {
	for _, schedule := range []string{
		"FREQ=WEEKLY;BYDAY=FR,SA,SU;BYSETPOS=-1",
		"FREQ=WEEKLY;BYHOUR=6;BYSETPOS=1",
		"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR,SA,SU;BYSETPOS=-1",
		"FREQ=MONTHLY;BYMONTHDAY=1,2,3,4,5,6,7;BYSETPOS=1",
		"FREQ=YEARLY;BYDAY=MO,SA;BYSETPOS=-1",
		"FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=30,31;BYSETPOS=-1",
	} {
		t.Run(schedule, func(t *testing.T) {
			requireAgingScheduleRejected(t, schedule, "Weekend exclusion would change BYSETPOS day candidates")
		})
	}
}
