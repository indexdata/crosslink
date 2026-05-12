//go:build cgo

package zoom

/*
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

type Query struct {
	zquery C.ZOOM_query
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

func NewPqfQuery(pqf string) (*Query, error) {
	cPqf := C.CString(pqf)
	defer C.free(unsafe.Pointer(cPqf))

	query := &Query{zquery: C.ZOOM_query_create()}
	runtime.SetFinalizer(query, (*Query).finalize)

	ret := C.ZOOM_query_prefix(query.zquery, cPqf)
	if ret != 0 {
		query.finalize()
		return nil, &ZoomError{Code: int(ret), Message: "failed to create PQF query"}
	}
	return query, nil
}

func NewCqlQuery(cql string) (*Query, error) {
	cCql := C.CString(cql)
	defer C.free(unsafe.Pointer(cCql))

	query := &Query{zquery: C.ZOOM_query_create()}
	runtime.SetFinalizer(query, (*Query).finalize)

	ret := C.ZOOM_query_cql(query.zquery, cCql)
	if ret != 0 {
		query.finalize()
		return nil, &ZoomError{Code: int(ret), Message: "failed to create CQL query"}
	}
	return query, nil
}

func (q *Query) finalize() {
	if q.zquery != nil {
		C.ZOOM_query_destroy(q.zquery)
		q.zquery = nil
	}
}

func (q *Query) Close() {
	q.finalize()
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
	if c.conn == nil {
		return &ZoomError{Code: 0, Message: "connection is not established"}
	}
	cHost := C.CString(host)
	defer C.free(unsafe.Pointer(cHost))
	C.ZOOM_connection_connect(c.conn, cHost, 0)
	return c.checkError()
}

func (c *Connection) Close() {
	c.finalize()
}

func (c *Connection) Search(query *Query) (*ResultSet, error) {
	if c.conn == nil {
		return nil, &ZoomError{Code: 0, Message: "connection is not established"}
	}
	if query.zquery == nil {
		return nil, &ZoomError{Code: 0, Message: "query is not initialized"}
	}
	cSet := C.ZOOM_connection_search(c.conn, query.zquery)
	set := &ResultSet{rs: cSet, connection: c}
	err := c.checkError()
	if err != nil {
		set.finalize()
		return nil, err
	}
	runtime.SetFinalizer(set, (*ResultSet).finalize)
	return set, nil
}

func (s *ResultSet) finalize() {
	if s.rs != nil {
		C.ZOOM_resultset_destroy(s.rs)
		s.rs = nil
	}
}

func (s *ResultSet) Close() {
	s.finalize()
}

func (s *ResultSet) Count() int {
	return int(C.ZOOM_resultset_size(s.rs))
}

func (c *Connection) checkError() error {
	if c.conn == nil {
		return nil
	}
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
		// non-surrogate diagnostic error for PresentResponse
		err := s.connection.checkError()
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

func (r *Record) Close() {
	r.finalize()
}

func (r *Record) Data(dataType string) []byte {
	if r.rec == nil {
		return nil
	}
	cType := C.CString(dataType)
	defer C.free(unsafe.Pointer(cType))
	cSize := C.int(0)
	cData := C.ZOOM_record_get(r.rec, cType, &cSize)
	if cData == nil || cSize <= 0 {
		return nil
	}
	// the returned cData is owned by record and will be freed when the record is destroyed
	data := C.GoBytes(unsafe.Pointer(cData), cSize)
	return data
}
