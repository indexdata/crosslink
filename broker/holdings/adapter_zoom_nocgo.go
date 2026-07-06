//go:build !cgo

package holdings

import (
	"fmt"

	"github.com/indexdata/crosslink/directory"
)

func cgoEnabled() bool { return false }

type ZoomAvailabilityAdapter struct{}

func NewZoomAvailabilityAdapter(config directory.ZoomConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	return nil, fmt.Errorf("ZOOM availability adapter requires cgo, but cgo is not enabled")
}
