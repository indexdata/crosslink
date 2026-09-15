package profiles

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.yaml
var builtinFiles embed.FS

type builtinProfile struct {
	LMS     object `json:"lmsConfig"`
	Catalog object `json:"catalogConfig"`
}

// Load once; merge copies nested maps before applying directory overrides.
var builtins = loadBuiltins()

func loadBuiltins() map[string]builtinProfile {
	files, err := builtinFiles.ReadDir("builtin")
	if err != nil {
		panic(err)
	}
	result := map[string]builtinProfile{}
	for _, file := range files {
		data, err := builtinFiles.ReadFile("builtin/" + file.Name())
		if err != nil {
			panic(err)
		}
		profile, err := decodeBuiltin(data)
		if err != nil {
			panic(fmt.Errorf("built-in profile %s: %w", file.Name(), err))
		}
		result[strings.TrimSuffix(file.Name(), ".yaml")] = profile
	}
	return result
}

func decodeBuiltin(data []byte) (builtinProfile, error) {
	var raw object
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return builtinProfile{}, err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return builtinProfile{}, err
	}
	// Validate names and types against the directory API, including nested fields.
	// Only host configuration sections belong in these profiles.
	var config struct {
		LMS     *dirapi.LmsConfig     `json:"lmsConfig"`
		Catalog *dirapi.CatalogConfig `json:"catalogConfig"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return builtinProfile{}, err
	}
	var profile builtinProfile
	err = json.Unmarshal(data, &profile)
	return profile, err
}
