package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kinbiko/jsonassert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"indexdata/directoryish/app"
)

var dbpool *pgxpool.Pool
var handler http.Handler

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("directoryish"),
		postgres.WithUsername("directoryish"),
		postgres.WithPassword("directoryish"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to start db container: %s", err))
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("failed to get conn string: %s", err))
	}

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../migrations"

	time.Sleep(1 * time.Second)

	app.RunMigrateScripts()
	dbpool = app.InitDbPool()
	defer dbpool.Close()
	handler = app.InitHandler(ctx, dbpool)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
	}
	os.Exit(code)
}

func TestEmptyGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/entries", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if string(data) != "[]\n" {
		t.Errorf("expected [] got %v", string(data))
	}
}

func TestEntryRoundtrip(t *testing.T) {
	ja := jsonassert.New(t)

	// POST authority
	body := `{
		"symbol": "RESHARE"
	}`
	req := httptest.NewRequest(http.MethodPost, "/authorities", bytes.NewBufferString(body))
	req.Header.Add("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	var postResult map[string]any
	err = json.Unmarshal([]byte(data), &postResult)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	postedId := postResult["id"].(string)
	if len(postedId) != 36 {
		t.Errorf("Did not find a 36 character ID property, instead got: %v", postedId)
	}

	// POST
	body = `{
		"name": "Some Inst",
		"contact_name": "Bob",
		"symbols": [
			{
				"symbol": "NWINST",
				"authority": "RESHARE"
			},
			{
				"symbol": "ALSO",
				"authority": "RESHARE"
			}
		]
	}`
	req = httptest.NewRequest(http.MethodPost, "/entries", bytes.NewBufferString(body))
	req.Header.Add("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res = w.Result()
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("Expected response status of 200, got %d and body of %s", res.StatusCode, data)
	}
	err = json.Unmarshal([]byte(data), &postResult)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	postedId = postResult["id"].(string)
	if len(postedId) != 36 {
		t.Errorf("Did not find a 36 character ID property, instead got: %v", postedId)
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/entries/"+postedId, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res = w.Result()
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("Expected response status of 200, got %d and body of %s", res.StatusCode, data)
	}
	ja.Assertf(string(data), `
	{
		"id": "%s",
		"name": "Some Inst",
		"contact_name": "Bob",
		"symbols": [
      "<<UNORDERED>>",
			{
				"id": "<<PRESENCE>>",
				"symbol": "NWINST",
				"authority": "RESHARE"
			},
			{
				"id": "<<PRESENCE>>",
				"symbol": "ALSO",
				"authority": "RESHARE"
			}
		]
	}
	`, postedId)
	var getResult map[string]any
	err = json.Unmarshal([]byte(data), &getResult)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	gotSymbols := getResult["symbols"].([]any)

	// PATCH
	filteredSymbols := filterAnyMapSlice(&gotSymbols, "symbol", "NWINST")
	body = fmt.Sprintf(`{
		"contact_name": null,
		"email": "info@someinst.edu",
		"symbols": [
			{
				"id":"%s",
				"symbol": "NEWINST",
				"authority": "RESHARE"
			}
		]
	}`, filteredSymbols[0].(map[string]any)["id"])
	req = httptest.NewRequest(http.MethodPatch, "/entries/"+postedId, bytes.NewBufferString(body))
	req.Header.Add("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res = w.Result()
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if res.StatusCode != 204 {
		t.Fatalf("Expected response status of 204, got %d and body of %s", res.StatusCode, data)
	}

	// GET again to confirm PATCH
	req = httptest.NewRequest(http.MethodGet, "/entries/"+postedId, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res = w.Result()
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("expected error to be nil got %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("Expected response status of 200, got %d and body of %s", res.StatusCode, data)
	}

	ja.Assertf(string(data), `
	{
		"id": "%s",
		"name": "Some Inst",
		"email": "info@someinst.edu",
		"symbols": [
			{
				"id": "<<PRESENCE>>",
				"symbol": "NEWINST",
				"authority": "RESHARE"
			}
		]
	}
	`, postedId)
}
