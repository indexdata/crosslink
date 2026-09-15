package prservice

import (
	"github.com/go-playground/validator/v10"
	"github.com/indexdata/crosslink/iso18626"
)

var illRequestValidator = validator.New(validator.WithRequiredStructEnabled())

// ValidateIllRequest applies the ISO request validation used by normal patron
// request creation. MultipleItemRequestId is optional in CrossLink's API even
// though the generated ISO model marks it as required.
func ValidateIllRequest(request iso18626.Request) error {
	requestForValidation := request
	if requestForValidation.Header.MultipleItemRequestId == "" {
		requestForValidation.Header.MultipleItemRequestId = "#empty"
	}
	return illRequestValidator.Struct(requestForValidation)
}
