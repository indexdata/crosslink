package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

var errorToThrow = errors.New("throwing error")
var errMsg = "this is test error message"
var ctx = CreateExtCtxWithArgs(context.Background(), nil)

func TestCreateExtCtxWithArgs(t *testing.T) {
	inArgs := &LoggerArgs{
		RequestId:     "test-request-id",
		TransactionId: "test-transaction-id",
		EventId:       "test-event-id",
		Component:     "test-component",
		Other:         map[string]string{"key": "value"},
	}
	extCtx := CreateExtCtxWithArgs(context.Background(), inArgs)

	assert.NotNil(t, extCtx)
	outArgs := extCtx.LoggerArgs()
	assert.Equal(t, inArgs, &outArgs)
	assert.NotNil(t, extCtx.Logger())
	outArgs.RequestId = "modified-request-id"
	extCtx2 := extCtx.WithArgs(&outArgs)
	outArgs2 := extCtx2.LoggerArgs()
	assert.Equal(t, outArgs, outArgs2)
	assert.NotEqual(t, inArgs, outArgs)
	assert.NotEqual(t, inArgs, outArgs2)
	assert.NotEqual(t, extCtx.LoggerArgs(), extCtx2.LoggerArgs())
	outArgs3 := outArgs2.WithComponent("new-component")
	assert.Equal(t, "new-component", outArgs3.Component)
	assert.NotEqual(t, outArgs2, *outArgs3)
}

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
	rr := httptest.NewRecorder()
	value := "return value"
	result := MustHttp(ctx, rr, func() (string, error) {
		return value, nil
	}, errMsg)
	assert.Equal(t, value, result)
}
func TestMustHttpWithErrorMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but no panic occurred")
		} else if r != errMsg {
			t.Errorf("expected panic with message '%v', got: %v", errMsg, r)
		}
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.Equal(t, errMsg+"\n", rr.Body.String())
	}()

	MustHttp(ctx, rr, func() (*string, error) {
		return nil, errorToThrow
	}, errMsg)
}

func TestMustHttpWithoutErrorMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but no panic occurred")
		} else if r != errorToThrow {
			t.Errorf("expected panic with message '%v', got: %v", errorToThrow, r)
		}
		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.Equal(t, "Internal server error\n", rr.Body.String())
	}()
	MustHttp(ctx, rr, func() (*string, error) {
		return nil, errorToThrow
	}, "")
}
