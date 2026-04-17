package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/oapi"
)

func ToLinkUrlValues(r *http.Request, urlValues url.Values) string {
	return ToLinkPath(r, r.URL.Path, urlValues.Encode())
}

func toLink(r *http.Request, path string, id string, query string) string {
	if strings.Contains(r.RequestURI, "/broker/") {
		path = "/broker" + path
	}
	if id != "" {
		path = path + "/" + id
	}
	return ToLinkPath(r, path, query)
}

func ToLinkPath(r *http.Request, path string, query string) string {
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
			link := ToLinkUrlValues(r, urlValues)
			about.LastLink = &link
		}
	}
	if offset64 > 0 {
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{"0"}
		firstLink := ToLinkUrlValues(r, urlValues)
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
		prevLink := ToLinkUrlValues(r, urlValues)
		about.PrevLink = &prevLink
	}
	if fullCount > offset64+limit64 {
		noffset := offset64 + limit64
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.FormatInt(noffset, 10)}
		link := ToLinkUrlValues(r, urlValues)
		about.NextLink = &link
	}
	return about
}
