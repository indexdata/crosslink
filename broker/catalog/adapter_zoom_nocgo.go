//go:build !cgo

package catalog

import (
	"fmt"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

func cgoEnabled() bool { return false }

type ZoomLookupAdapter struct{}

func NewZoomLookupAdapter(config dirapi.ZoomConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	return nil, fmt.Errorf("ZOOM lookup adapter requires cgo, but cgo is not enabled")
}
