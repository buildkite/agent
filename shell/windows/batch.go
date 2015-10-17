package windows

import "strings"

// Escapes a string for use with an `ECHO` statement in a Batch file.
// http://www.robvanderwoude.com/escapechars.php
func BatchEscape(str string) string {
	str = strings.Replace(str, "%", "%%", -1)
	str = strings.Replace(str, "^", "^^", -1)
	str = strings.Replace(str, "^", "^^", -1)
	str = strings.Replace(str, "&", "^&", -1)
	str = strings.Replace(str, "<", "^<", -1)
	str = strings.Replace(str, ">", "^>", -1)
	str = strings.Replace(str, "|", "^|", -1)

	return str
}
