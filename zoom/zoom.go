// file: zoom.go
//go:build cgo

package zoom

/*
#cgo pkg-config: yaz
#include <yaz/zoom.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

type Options map[string]string

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
	code := C.ZOOM_connection_error(c.conn, nil, nil)
	if code != 0 {
		return fmt.Errorf("failed to connect: %s", C.GoString(C.ZOOM_connection_errmsg(c.conn)))
	}
	return nil
}

func (c *Connection) Close() {
	if c.conn != nil {
		C.ZOOM_connection_close(c.conn)
	}
}

func (c *Connection) Search(query string) (*ResultSet, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("connection is not established")
	}
	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))
	cSet := C.ZOOM_connection_search_pqf(c.conn, cQuery)
	set := &ResultSet{rs: cSet, connection: c}
	runtime.SetFinalizer(set, (*ResultSet).finalize)
	code := C.ZOOM_connection_error(c.conn, nil, nil)
	if code != 0 {
		return nil, fmt.Errorf("search failed: %s", C.GoString(C.ZOOM_connection_errmsg(c.conn)))
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

func (s *ResultSet) GetRecord(index int) (*Record, error) {
	if index < 0 || index >= s.Count() {
		return nil, nil
	}
	cRecord := C.ZOOM_resultset_record(s.rs, C.size_t(index))
	if cRecord == nil {
		return nil, nil
	}
	code := C.ZOOM_connection_error(s.connection.conn, nil, nil)
	if code != 0 {
		return nil, fmt.Errorf("get record failed: %s", C.GoString(C.ZOOM_connection_errmsg(s.connection.conn)))
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
