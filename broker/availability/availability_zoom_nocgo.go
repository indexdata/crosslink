//go:build !cgo

package availability

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
)

func cgoEnabled() bool { return false }

type ZoomAvailabilityAdapter struct{}

func NewZoomAvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config, queryBuilder adapter.LookupQueryBuilder, holdingsParser adapter.HoldingsParser) (adapter.LookupAdapter, error) {
	return nil, fmt.Errorf("ZOOM availability adapter requires cgo, but cgo is not enabled")
}
