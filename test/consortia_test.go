package test

import (
	"context"
	"net/http"
	"testing"
)

func TestConsortiumCases(t *testing.T) {
	cases := []httpTestCase{
		{
			name:     "GET consortium",
			method:   http.MethodGet,
			endpoint: "/consortia/00000000-0000-0000-0000-000000000001",
			status:   http.StatusOK,
			resFile:  "consortium.get.res.json",
		},
		{
			name:     "GET id not found",
			method:   http.MethodGet,
			endpoint: "/entries/by-id/f0000000-0000-0000-0000-000000000002",
			status:   http.StatusNotFound,
		},
		{
			name:     "GET consortia",
			method:   http.MethodGet,
			endpoint: "/consortia",
			status:   http.StatusOK,
			resFile:  "consortia.get.res.json",
		},
		{
			name:        "POST consortium",
			method:      http.MethodPost,
			endpoint:    "/consortia",
			status:      http.StatusCreated,
			bodyFile:    "consortium.post.req.json",
			refetchFile: "consortium.post.refetch.json",
		},
		{
			name:        "PATCH consortium",
			method:      http.MethodPatch,
			endpoint:    "/consortia/00000000-0000-0000-0000-000000000001",
			status:      http.StatusNoContent,
			bodyFile:    "consortium.patch.req.json",
			refetchFile: "consortium.patch.refetch.json",
		},
		{
			name:     "PATCH id not found",
			method:   http.MethodPatch,
			endpoint: "/consortia/f0000000-0000-0000-0000-000000000002",
			bodyFile: "consortium.patch.req.json",
			status:   http.StatusNotFound,
		},
		{
			name:          "DELETE consortium",
			method:        http.MethodDelete,
			endpoint:      "/consortia/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
		},
		{
			name:     "POST consortium with non-existent entry FK",
			method:   http.MethodPost,
			endpoint: "/consortia",
			status:   http.StatusInternalServerError,
			body:     `{"name":"Test Consortium","entry":"f0000000-0000-0000-0000-000000000099"}`,
		},
		{
			name:     "PATCH consortium.entry to non-existent UUID",
			method:   http.MethodPatch,
			endpoint: "/consortia/00000000-0000-0000-0000-000000000001",
			status:   http.StatusInternalServerError,
			body:     `{"entry":"f0000000-0000-0000-0000-000000000099"}`,
		},
		{
			name:     "DELETE entry referenced by consortium verifies SET NULL",
			method:   http.MethodDelete,
			endpoint: "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:   http.StatusNoContent,
			resFunc: func(res *http.Response, data string) bool {
				if res.StatusCode != http.StatusNoContent {
					return false
				}
				// Verify consortium still exists but entry is null
				var entryID *string
				err := dbpool.QueryRow(context.Background(),
					"SELECT entry FROM consortia WHERE id = '00000000-0000-0000-0000-000000000001'").Scan(&entryID)
				return err == nil && entryID == nil
			},
		},
	}
	testCases(t, cases)
}
