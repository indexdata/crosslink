package prservice

import "github.com/indexdata/crosslink/broker/common"

// SupplierRelocator performs fresh holdings lookup and supplier selection for a
// retry request that carries updated bibliographic information.
type SupplierRelocator interface {
	// RelocateSuppliers synchronously runs locate-suppliers and select-supplier for
	// the ILL transaction identified by the given requesterRequestID.
	RelocateSuppliers(ctx common.ExtendedContext, requesterRequestID string) error
}
