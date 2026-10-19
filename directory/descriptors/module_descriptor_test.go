package descriptors_test

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/directory/api"
)

type moduleDescriptor struct {
	Provides []struct {
		Handlers []struct {
			Methods     []string `json:"methods"`
			PathPattern string   `json:"pathPattern"`
		} `json:"handlers"`
	} `json:"provides"`
}

func TestHandlersMatchOpenAPI(t *testing.T) {
	descriptorData, err := os.ReadFile("ModuleDescriptor-template.json")
	if err != nil {
		t.Fatal(err)
	}

	var descriptor moduleDescriptor
	if err := json.Unmarshal(descriptorData, &descriptor); err != nil {
		t.Fatal(err)
	}

	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Servers) != 1 {
		t.Fatalf("expected exactly one OpenAPI server, got %d", len(spec.Servers))
	}

	basePath := strings.TrimSuffix(spec.Servers[0].URL, "/")
	expected := make(map[string]struct{})
	for path, pathItem := range spec.Paths.Map() {
		for method := range pathItem.Operations() {
			expected[routeKey(method, basePath+path)] = struct{}{}
		}
	}

	actual := make(map[string]struct{})
	for _, provided := range descriptor.Provides {
		for _, handler := range provided.Handlers {
			if strings.Contains(handler.PathPattern, ".") {
				t.Errorf("Okapi pathPattern must not contain periods: %s", handler.PathPattern)
			}
			paths := []string{handler.PathPattern}
			if handler.PathPattern == basePath+"/openapi{extension}" {
				// Okapi's placeholder consumes the suffix, including the period.
				paths = []string{basePath + "/openapi.yaml", basePath + "/openapi.json"}
			}
			for _, method := range handler.Methods {
				for _, path := range paths {
					key := routeKey(method, path)
					if _, duplicate := actual[key]; duplicate {
						t.Errorf("duplicate module descriptor handler %s", key)
					}
					actual[key] = struct{}{}
				}
			}
		}
	}

	missing := difference(expected, actual)
	stale := difference(actual, expected)
	if len(missing) != 0 || len(stale) != 0 {
		t.Errorf("module descriptor does not match OpenAPI\nmissing handlers: %v\nstale handlers: %v", missing, stale)
	}
}

func routeKey(method, path string) string {
	return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
}

func difference(left, right map[string]struct{}) []string {
	result := make([]string, 0)
	for value := range left {
		if _, found := right[value]; !found {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
