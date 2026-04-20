package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/oapi"
)

func IsBrokerRequest(r *http.Request) bool {
	return strings.Contains(r.RequestURI, "/broker/")
}

func WithBrokerPrefix(r *http.Request, path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if IsBrokerRequest(r) {
		if path == "/" {
			return "/broker/"
		}
		return "/broker" + path
	}
	return path
}

func LinkRel(r *http.Request, relPath string, urlValues url.Values) string {
	path := r.URL.Path
	cleanRelPath := strings.Trim(relPath, "/")
	if cleanRelPath != "" {
		path = strings.TrimRight(path, "/") + "/" + cleanRelPath
	}
	return linkRaw(r, path, urlValues.Encode())
}

func Path(parts ...string) string {
	pathParts := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := strings.Trim(part, "/")
		if clean != "" {
			pathParts = append(pathParts, url.PathEscape(clean))
		}
	}
	return "/" + strings.Join(pathParts, "/")
}

func Query(params ...string) url.Values {
	values := url.Values{}
	for i := 0; i+1 < len(params); i += 2 {
		values.Add(params[i], params[i+1])
	}
	return values
}

func Link(r *http.Request, path string, query url.Values) string {
	return linkRaw(r, WithBrokerPrefix(r, path), query.Encode())
}

func linkRaw(r *http.Request, path string, query string) string {
	if query != "" {
		path = path + "?" + query
	}
	urlScheme := r.Header.Get("X-Forwarded-Proto")
	if len(urlScheme) == 0 {
		urlScheme = r.URL.Scheme
	}
	if len(urlScheme) == 0 {
		urlScheme = "https"
	}
	urlHost := r.Header.Get("X-Forwarded-Host")
	if len(urlHost) == 0 {
		urlHost = r.URL.Host
	}
	if len(urlHost) == 0 {
		urlHost = r.Host
	}
	if strings.Contains(urlHost, "localhost") {
		urlScheme = "http"
	}
	return urlScheme + "://" + urlHost + path
}

func CollectAboutData(fullCount int64, offset int32, limit int32, r *http.Request) oapi.About {
	about := oapi.About{}
	about.Count = fullCount
	if limit <= 0 {
		return about
	}
	limit64 := int64(limit)
	offset64 := int64(offset)
	lastOffset := int64(0)
	if fullCount > 0 {
		lastOffset = ((fullCount - 1) / limit64) * limit64
	}
	if fullCount > limit64 {
		if offset64 != lastOffset {
			urlValues := r.URL.Query()
			urlValues["offset"] = []string{strconv.FormatInt(lastOffset, 10)}
			link := LinkRel(r, "", urlValues)
			about.LastLink = &link
		}
	}
	if offset64 > 0 {
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{"0"}
		firstLink := LinkRel(r, "", urlValues)
		about.FirstLink = &firstLink

		pOffset := offset64 - limit64
		if pOffset < 0 {
			pOffset = 0
		}
		if pOffset > lastOffset {
			pOffset = lastOffset
		}
		urlValues = r.URL.Query()
		urlValues["offset"] = []string{strconv.FormatInt(pOffset, 10)}
		prevLink := LinkRel(r, "", urlValues)
		about.PrevLink = &prevLink
	}
	if fullCount > offset64+limit64 {
		noffset := offset64 + limit64
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.FormatInt(noffset, 10)}
		link := LinkRel(r, "", urlValues)
		about.NextLink = &link
	}
	return about
}
