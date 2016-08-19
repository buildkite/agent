// +build linux darwin freebsd

package proctitle

import (
	"os"
	"reflect"
	"unsafe"
)

// We capture the location and length of argv0 early because Go's might move
var argv0str = (*reflect.StringHeader)(unsafe.Pointer(&os.Args[0]))
var argv0 = (*[1 << 30]byte)(unsafe.Pointer(argv0str.Data))[:argv0str.Len]

// Now we can repeatedly replace argv0 within its original position and length
func Replace(title string) {
	n := copy(argv0, title)
	for n < argv0str.Len {
		argv0[n] = 0
		n += 1
	}
}
