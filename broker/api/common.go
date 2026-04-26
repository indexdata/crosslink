package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
)

func withBasePath(r *http.Request, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if tenant.IsOkapiRequest(r) {
		return tenant.OKAPI_PATH_PREFIX + path
	}
	return path
}

func Path(parts ...string) string {
	return path(true, parts...)
}

func path(escape bool, parts ...string) string {
	pathParts := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := strings.Trim(part, "/")
		if clean != "" {
			if escape {
				pathParts = append(pathParts, url.PathEscape(clean))
			} else {
				pathParts = append(pathParts, clean)
			}
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
	return link(r, withBasePath(r, path), query.Encode())
}

func LinkRel(r *http.Request, relPath string, urlValues url.Values) string {
	return link(r, path(false, r.URL.Path, relPath), urlValues.Encode())
}

func hostOnly(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(h)
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return strings.ToLower(host)
}

func isLocalHost(host string) bool {
	host = hostOnly(host)
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func getHost(r *http.Request) string {
	first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	host := strings.TrimSpace(first)
	if host == "" {
		host = strings.TrimSpace(r.URL.Host)
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	return host
}

func getProto(r *http.Request) string {
	first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	proto := strings.TrimSpace(first)
	proto = strings.ToLower(proto)
	if proto == "" {
		proto = strings.ToLower(strings.TrimSpace(r.URL.Scheme))
	}
	if proto != "http" && proto != "https" {
		proto = "https"
	}
	if isLocalHost(getHost(r)) {
		return "http"
	}
	return proto
}

func link(r *http.Request, path string, query string) string {
	if query != "" {
		path = path + "?" + query
	}
	scheme := getProto(r)
	host := getHost(r)
	return scheme + "://" + host + path
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
			params := r.URL.Query()
			params["offset"] = []string{strconv.FormatInt(lastOffset, 10)}
			link := LinkRel(r, "", params)
			about.LastLink = &link
		}
	}
	if offset64 > 0 {
		params := r.URL.Query()
		params["offset"] = []string{"0"}
		firstLink := LinkRel(r, "", params)
		about.FirstLink = &firstLink

		pOffset := offset64 - limit64
		if pOffset < 0 {
			pOffset = 0
		}
		if pOffset > lastOffset {
			pOffset = lastOffset
		}
		params = r.URL.Query()
		params["offset"] = []string{strconv.FormatInt(pOffset, 10)}
		prevLink := LinkRel(r, "", params)
		about.PrevLink = &prevLink
	}
	if fullCount > offset64+limit64 {
		noffset := offset64 + limit64
		params := r.URL.Query()
		params["offset"] = []string{strconv.FormatInt(noffset, 10)}
		link := LinkRel(r, "", params)
		about.NextLink = &link
	}
	return about
}
