//go:build !cgo

package catalog

import (
	"fmt"

	"github.com/indexdata/crosslink/directory"
)

func cgoEnabled() bool { return false }

type ZoomLookupAdapter struct{}

func NewZoomLookupAdapter(config directory.ZoomConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	return nil, fmt.Errorf("ZOOM lookup adapter requires cgo, but cgo is not enabled")
}
