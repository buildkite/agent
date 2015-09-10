// +build linux,cgo

package agent

import (
	"strings"

	"github.com/ErikDubbelboer/gspt"
)

func SetProcTitle(title string) {
	length := len(title)

	if length >= 255 {
		length = 255
		gspt.SetProcTitle(title[:255])
	} else {
		title += strings.Repeat(" ", 255-length)
		gspt.SetProcTitle(title)
	}
}
