package test

func filterAnyMapSlice(arr *[]any, key string, value string) []any {
	res := []any{}

	for i := range *arr {
		if (*arr)[i].(map[string]any)[key] == value {
			res = append(res, (*arr)[i])
		}
	}

	return res
}
