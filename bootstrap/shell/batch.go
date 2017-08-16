package shell

import "strings"

// BatchEscape escapes a string for use with an `ECHO` statement in a Windows Batch file.
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
