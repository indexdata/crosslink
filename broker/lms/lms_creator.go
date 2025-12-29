package lms

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5/pgtype"
)

// LmsCreator is an interface for creating LMS adapters based on a symbol
// Symbol is used to look up directory entry with information about LMS in use
type LmsCreator interface {
	GetAdapter(ctx common.ExtendedContext, symbol pgtype.Text) (LmsAdapter, error)
}
