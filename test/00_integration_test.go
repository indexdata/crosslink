package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-testfixtures/testfixtures/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/kinbiko/jsonassert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"indexdata/directory/app"
)

var dbpool *pgxpool.Pool
var handler http.Handler
var fixtures *testfixtures.Loader

func jsonReq(t *testing.T, method string, endpoint string, bodyStr string, addlHeaders map[string]string) (*http.Response, string) {
	var req *http.Request
	fullPath := app.BasePath + endpoint
	if bodyStr != "" {
		req = httptest.NewRequest(method, fullPath, bytes.NewBufferString(bodyStr))
	} else {
		req = httptest.NewRequest(method, fullPath, nil)
	}
	req.Header.Add("Content-Type", "application/json")

	if addlHeaders != nil {
		for key, val := range addlHeaders {
			req.Header.Add(key, val)
		}
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	res := w.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("Unexpected error reading response body %v", err)
	}
	return res, string(data)
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("directory_test"),
		postgres.WithUsername("directory"),
		postgres.WithPassword("directory"),
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

	// Set up fixtures so we can initialise the db with some test data
	// These aren't aware of pgx so we need another connection to the db
	// with the db/sql interface
	stdDb, err := sql.Open("pgx", connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer stdDb.Close()

	fixtures, err = testfixtures.New(
		testfixtures.Database(stdDb),
		testfixtures.Dialect("postgres"),
		testfixtures.Directory("dbfixtures"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failure initialising DB fixtures: %v\n", err)
		os.Exit(1)
	}

	handler = app.InitHandler(ctx, dbpool)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
	}
	os.Exit(code)
}

func resetDb() {
	if err := fixtures.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "problem loading fixtures: %v\n", err)
	}
}

var standardHeaders = map[string]string{
	"X-Okapi-Tenant":      "ANINST",
	"X-Okapi-Permissions": `["directory.consortium.all"]`,
}

// Before loading fixtures let's confirm endpoints are able to handle the case
// where the db has no records
func TestEmptyGet(t *testing.T) {
	endpoints := map[string]string{
		"/entries":   `{"about":{"count":0},"items":[]}`,
		"/consortia": "[]",
	}
	for endpoint, expected := range endpoints {
		res, data := jsonReq(t, http.MethodGet, endpoint, "", standardHeaders)
		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected response status of 200 for %s, got %d and body of %s", endpoint, res.StatusCode, data)
		}
		if string(data) != expected+"\n" {
			t.Errorf("expected %s for %s got %v", expected, endpoint, string(data))
		}
	}
}

func testCase(t *testing.T, c httpTestCase) {
	resetDb()
	ja := jsonassert.New(t)
	pre := "Case " + c.name + " -- "

	var body string
	var err error
	if c.bodyTmpl != nil {
		body, err = loadApiTmpl(c.refetchFile, c.bodyTmpl)
		if err != nil {
			t.Errorf(pre+"Error loading body fixture: %v", err)
		}
	} else if c.bodyFile != "" {
		body, err = loadApiFixture(c.bodyFile)
		if err != nil {
			t.Errorf(pre+"Error loading body fixture: %v", err)
		}
	} else {
		body = c.body
	}

	res, data := jsonReq(t, c.method, c.endpoint, body, c.addlHeaders)

	if c.status != 0 && res.StatusCode != c.status {
		t.Errorf(pre+"Expected response status of %d, got %d and body of %s", c.status, res.StatusCode, data)
	}

	if c.resFile != "" {
		expectedResponse, err := loadApiFixture(c.resFile)
		if err != nil {
			t.Errorf(pre+"expected error to be nil got %v", err)
		}
		ja.Assertf(data, expectedResponse)
	} else if c.res != "" {
		if data != c.res {
			t.Errorf(pre+"Expected %v got %v", c.res, data)
		}
	}

	if c.resFunc != nil {
		if c.resFunc(res, data) != true {
			t.Error(pre + "resFunc returned false")
		}
	}

	var idOfPosted string
	if c.method == http.MethodPost && c.refetchFile != "" {
		var postResult map[string]any
		err = json.Unmarshal([]byte(data), &postResult)
		if err != nil {
			t.Errorf(pre+"Error parsing POST response to get ID: %v", err)
		}
		idOfPosted = postResult["id"].(string)
		if len(idOfPosted) != 36 {
			t.Errorf(pre+"Did not find a 36 character ID property, instead got: %v", idOfPosted)
		}
	}

	if c.refetchFile != "" || c.refetchStatus != 0 {
		var refetchEndpoint string
		if c.refetchEndpoint != "" {
			refetchEndpoint = c.refetchEndpoint
		} else {
			refetchEndpoint = c.endpoint
		}
		if idOfPosted != "" {
			refetchEndpoint = refetchEndpoint + "/" + idOfPosted
		}
		reres, redata := jsonReq(t, http.MethodGet, refetchEndpoint, "", c.addlHeaders)
		if c.refetchStatus != 0 {
			if reres.StatusCode != c.refetchStatus {
				t.Errorf(pre+"Expected response status of %d when refetching, got %d and body of %s", c.refetchStatus, reres.StatusCode, redata)
			}
		} else if reres.StatusCode >= 400 {
			t.Errorf(pre+"Expected response status of OK when refetching, got %d and body of %s", reres.StatusCode, redata)
		}

		if c.refetchFile != "" {
			var expectedRefetchResponse string
			if idOfPosted != "" {
				expectedRefetchResponse, err = loadApiTmpl(c.refetchFile, map[string]any{"id": idOfPosted})
				if err != nil {
					t.Errorf(pre+"Error loading fixture to re-fetch: %v", err)
				}
			} else {
				expectedRefetchResponse, err = loadApiFixture(c.refetchFile)
				if err != nil {
					t.Errorf(pre+"Error loading fixture to re-fetch: %v", err)
				}

			}
			ja.Assertf(redata, expectedRefetchResponse)
		}
	}
}

type httpTestCase struct {
	name            string
	method          string
	endpoint        string
	body            string
	bodyFile        string         // if nonempty this file in apifixtures will be preferred
	bodyTmpl        map[string]any // bodyFile to be treated as template with these values
	status          int
	res             string
	resFile         string
	resFunc         func(*http.Response, string) bool // if defined will need to evaluate to true when passed res
	refetchFile     string                            // if nonempty a GET will be performed and compared to this
	refetchEndpoint string                            // alternate endpoint prefix to id for refetch
	refetchStatus   int
	addlHeaders     map[string]string
}

func testCases(t *testing.T, cases []httpTestCase) {
	for _, c := range cases {
		testCase(t, c)
	}
}
