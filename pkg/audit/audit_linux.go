// +build linux
/*
  The audit package is a go bindings to libaudit that only allows for
  logging audit events.

  Author Steve Grubb <sgrubb@redhat.com>
*/

package audit

// #cgo LDFLAGS: -laudit
// #include "libaudit.h"
// #include <unistd.h>
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	AUDIT_VIRT_CONTROL    = 2500
	AUDIT_VIRT_RESOURCE   = 2501
	AUDIT_VIRT_MACHINE_ID = 2502
)

func AuditValueNeedsEncoding(str string) bool {
	cstr := C.CString(str)
	defer C.free(unsafe.Pointer(cstr))
	len := C.strlen(cstr)

	res, _ := C.audit_value_needs_encoding(cstr, C.uint(len))
	if res != 0 {
		return true
	}
	return false
}

func AuditEncodeNVString(name string, value string) string {
	cname := C.CString(name)
	cval := C.CString(value)

	cres := C.audit_encode_nv_string(cname, cval, 0)

	C.free(unsafe.Pointer(cname))
	C.free(unsafe.Pointer(cval))
	defer C.free(unsafe.Pointer(cres))

	return C.GoString(cres)
}

func AuditLogUserEvent(event_type int, message string, result bool) error {
	var r int
	fd := C.audit_open()
	defer C.close(fd)
	if result {
		r = 1
	} else {
		r = 0
	}
	if fd > 0 {
		cmsg := C.CString(message)
		defer C.free(unsafe.Pointer(cmsg))
		_, err := C.audit_log_user_message(fd, C.int(event_type), cmsg, nil, nil, nil, C.int(r))
		return err
	}
	return nil
}

func AuditFormatVars(vars map[string]string) string {
	var result string
	for key, value := range vars {
		result += fmt.Sprintf("%s=%s ", key, value)
	}
	return result
}
