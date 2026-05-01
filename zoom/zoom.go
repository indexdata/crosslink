// file: zoom.go
//go:build cgo

package zoom

/*
// file: zoom.go
#cgo pkg-config: yaz
#include <yaz/zoom.h>
#include <stdlib.h>
*/
import "C"
import (
	"runtime"
	"unsafe"
)

type Options map[string]string

type ZoomError struct {
	Code           int
	Message        string
	AdditionalInfo string
}

func (e *ZoomError) Error() string {
	msg := "zoom error: "
	if e.Message != "" {
		msg += e.Message
	}
	if e.AdditionalInfo != "" {
		msg += " (" + e.AdditionalInfo + ")"
	}
	return msg
}

type Connection struct {
	conn C.ZOOM_connection
}

type ResultSet struct {
	connection *Connection
	rs         C.ZOOM_resultset
}

type Record struct {
	rec C.ZOOM_record
}

func (o *Options) toZoomOptions() C.ZOOM_options {
	zo := C.ZOOM_options_create()
	for key, value := range *o {
		cKey := C.CString(key)
		cValue := C.CString(value)
		C.ZOOM_options_set(zo, cKey, cValue)
		C.free(unsafe.Pointer(cKey))
		C.free(unsafe.Pointer(cValue))
	}
	return zo
}

func NewConnection(options Options) *Connection {
	c := &Connection{}
	cOptions := options.toZoomOptions()
	defer C.ZOOM_options_destroy(cOptions)
	c.conn = C.ZOOM_connection_create(cOptions)
	runtime.SetFinalizer(c, (*Connection).finalize)
	return c
}

func (c *Connection) finalize() {
	if c.conn != nil {
		C.ZOOM_connection_destroy(c.conn)
		c.conn = nil
	}
}

func (c *Connection) Connect(host string) error {
	cHost := C.CString(host)
	defer C.free(unsafe.Pointer(cHost))
	C.ZOOM_connection_connect(c.conn, cHost, 0)
	return c.checkError()
}

func (c *Connection) Close() {
	if c.conn != nil {
		C.ZOOM_connection_close(c.conn)
	}
}

func (c *Connection) Search(query string) (*ResultSet, error) {
	if c.conn == nil {
		return nil, &ZoomError{Code: 0, Message: "connection is not established"}
	}
	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))
	cSet := C.ZOOM_connection_search_pqf(c.conn, cQuery)
	set := &ResultSet{rs: cSet, connection: c}
	runtime.SetFinalizer(set, (*ResultSet).finalize)
	err := c.checkError()
	if err != nil {
		return nil, err
	}
	return set, nil
}

func (s *ResultSet) finalize() {
	if s.rs != nil {
		C.ZOOM_resultset_destroy(s.rs)
		s.rs = nil
	}
}

func (s *ResultSet) Count() int {
	return int(C.ZOOM_resultset_size(s.rs))
}

func (c *Connection) checkError() error {
	var cErrMsg, cAddInfo *C.char
	code := C.ZOOM_connection_error(c.conn, (**C.char)(unsafe.Pointer(&cErrMsg)), (**C.char)(unsafe.Pointer(&cAddInfo)))
	if code != 0 {
		var errMsg, addInfo string
		if cErrMsg != nil {
			errMsg = C.GoString(cErrMsg)
		}
		if cAddInfo != nil {
			addInfo = C.GoString(cAddInfo)
		}
		return &ZoomError{Code: int(code), Message: errMsg, AdditionalInfo: addInfo}
	}
	return nil
}

func (s *ResultSet) GetRecord(index int) (*Record, error) {
	if s.rs == nil {
		return nil, &ZoomError{Code: 0, Message: "result set is not available"}
	}
	if index < 0 || index >= s.Count() {
		return nil, nil
	}
	cRecord := C.ZOOM_resultset_record(s.rs, C.size_t(index))
	if cRecord == nil {
		return nil, nil
	}
	err := s.connection.checkError()
	if err != nil {
		return nil, err
	}
	// check for surrogate diagnostic
	var cErrMsg, cAddInfo *C.char
	code := C.ZOOM_record_error(cRecord, (**C.char)(unsafe.Pointer(&cErrMsg)), (**C.char)(unsafe.Pointer(&cAddInfo)), nil)
	if code != 0 {
		var errMsg, addInfo string
		if cErrMsg != nil {
			errMsg = C.GoString(cErrMsg)
		}
		if cAddInfo != nil {
			addInfo = C.GoString(cAddInfo)
		}
		return nil, &ZoomError{Code: int(code), Message: errMsg, AdditionalInfo: addInfo}
	}
	record := &Record{rec: C.ZOOM_record_clone(cRecord)}
	runtime.SetFinalizer(record, (*Record).finalize)
	return record, nil
}

func (r *Record) finalize() {
	if r.rec != nil {
		C.ZOOM_record_destroy(r.rec)
		r.rec = nil
	}
}

func (r *Record) Data(dataType string) string {
	if r.rec == nil {
		return ""
	}
	cType := C.CString(dataType)
	defer C.free(unsafe.Pointer(cType))
	cData := C.ZOOM_record_get(r.rec, cType, nil)
	if cData == nil {
		return ""
	}
	// the returned cData is owned by record and will be freed when the record is destroyed
	data := C.GoString(cData)
	return data
}
