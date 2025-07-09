package shell

import "strings"

var echoEscaper = strings.NewReplacer(
	"%", "%%",
	"^", "^^",
	"^", "^^",
	"&", "^&",
	"<", "^<",
	">", "^>",
	"|", "^|",
)

// BatchEscape escapes a string for use with an `ECHO` statement in a Windows Batch file.
// http://www.robvanderwoude.com/escapechars.php
func BatchEscape(str string) string {
	return echoEscaper.Replace(str)
}
