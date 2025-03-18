package extctx

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"net/http/httptest"
	"testing"
)

var errorToThrow = errors.New("throwing error")
var errMsg = "this is test error message"
var ctx = CreateExtCtxWithArgs(context.Background(), nil)

func TestMust(t *testing.T) {
	value := "return value"
	result := Must(ctx, func() (string, error) {
		return value, nil
	}, errMsg)
	assert.Equal(t, value, result)
}
func TestMustWithErrorMessage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but no panic occurred")
		} else if r != errMsg {
			t.Errorf("expected panic with message '%v', got: %v", errMsg, r)
		}
	}()
	Must(ctx, func() (*string, error) {
		return nil, errorToThrow
	}, errMsg)
}

func TestMustWithoutErrorMessage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but no panic occurred")
		} else if r != errorToThrow {
			t.Errorf("expected panic with message '%v', got: %v", errorToThrow, r)
		}
	}()
	Must(ctx, func() (*string, error) {
		return nil, errorToThrow
	}, "")
}

func TestMustHttp(t *testing.T) {
	rr := httptest.NewRecorder() // This should work but it does not
	value := "return value"
	result := MustHttp(ctx, rr, func() (string, error) {
		return value, nil
	}, errMsg)
	assert.Equal(t, value, result)
}
