//nolint:unused
package test

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed apifixtures
var apifixtures embed.FS

func loadApiFixture(filename string) (string, error) {
	bytes, err := apifixtures.ReadFile("apifixtures/" + filename)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func loadApiTmpl(filename string, data any) (string, error) {
	tmplString, err := loadApiFixture(filename)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(filename).Parse(tmplString)
	if err != nil {
		return "", err
	}
	buf := bytes.Buffer{}
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func filterAnyMapSlice(arr *[]any, key string, value string) []any {
	res := []any{}

	for i := range *arr {
		if (*arr)[i].(map[string]any)[key] == value {
			res = append(res, (*arr)[i])
		}
	}

	return res
}
