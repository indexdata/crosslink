package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
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
	if fullCount > limit64 {
		lastOffset := ((fullCount - 1) / limit64) * limit64
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.FormatInt(lastOffset, 10)}
		link := ToLinkUrlValues(r, urlValues)
		about.LastLink = &link
	}
	if offset64 > 0 {
		pOffset := offset64 - limit64
		if pOffset < 0 {
			pOffset = 0
		}
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.FormatInt(pOffset, 10)}
		link := ToLinkUrlValues(r, urlValues)
		about.PrevLink = &link
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

func GetSymbolForRequest(r *http.Request, tenantResolver common.Tenant, tenant *string, symbol *string) (string, error) {
	if strings.Contains(r.RequestURI, "/broker/") {
		if tenantResolver.IsSpecified() {
			if tenant == nil {
				return "", errors.New("X-Okapi-Tenant must be specified")
			} else {
				return tenantResolver.GetSymbol(*tenant), nil
			}
		} else {
			return "", errors.New("tenant mapping must be specified")
		}
	} else {
		if symbol == nil || *symbol == "" {
			return "", errors.New("symbol must be specified")
		} else {
			return *symbol, nil
		}
	}
}
