package evaluator

import (
	"fmt"
	"strings"
)

var traceLevel int = 0
var traceEnabled bool = false

const traceIdentPlaceholder string = "\t"

func identLevel() string {
	return strings.Repeat(traceIdentPlaceholder, traceLevel-1)
}

func tracePrint(fs string) {
	if traceEnabled {
		fmt.Printf("%s%s\n", identLevel(), fs)
	}
}

func incIdent() { traceLevel = traceLevel + 1 }
func decIdent() { traceLevel = traceLevel - 1 }

func trace(msg string, v ...interface{}) string {
	incIdent()
	tracePrint(fmt.Sprintf("BEGIN %s: %+v", msg, v))
	return msg
}

func untrace(msg string) {
	tracePrint("END " + msg)
	decIdent()
}
