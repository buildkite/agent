package cache

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Emit this report in one logger call so concurrent restores cannot interleave rows.
func restoreReport(cacheID string, result RestoreResult, err error) string {
	var report strings.Builder
	fmt.Fprintf(&report, "Restoring cache: %s\n", logValue(cacheID))
	denied, incomplete := false, false
	if diagnostics := result.Diagnostics; diagnostics != nil {
		if len(diagnostics.Attempts) == 0 {
			fmt.Fprintf(&report, "  Key: %s\n", logValue(strings.Join(diagnostics.CacheKey, "-")))
			for _, scopes := range diagnostics.ScopeCandidates {
				fmt.Fprintf(&report, "  %s → not searched\n", displayScopes(scopes))
			}
		}
		width := 0
		for _, attempt := range diagnostics.Attempts {
			width = max(width, utf8.RuneCountInString(logValue(strings.Join(attempt.CacheKey, "-"))))
		}
		for _, attempt := range diagnostics.Attempts {
			fmt.Fprintf(&report, "  %-*s · %s → %s", width,
				logValue(strings.Join(attempt.CacheKey, "-")), displayScopes(attempt.Scopes), logValue(attempt.Outcome))
			switch attempt.Outcome {
			case "denied":
				denied = true
				if attempt.Rule == "" {
					report.WriteString(" (no matching allow rule)")
				} else {
					fmt.Fprintf(&report, " (rule: %s)", logValue(attempt.Rule))
				}
			case "incomplete":
				incomplete = true
			}
			report.WriteByte('\n')
		}
		if diagnostics.BudgetExhausted {
			report.WriteString("  Registry search budget exhausted; some candidates may not have been fully searched\n")
			incomplete = true
		}
	} else if result.Key != "" {
		fmt.Fprintf(&report, "  Key: %s (search diagnostics unavailable)\n", logValue(result.Key))
	}

	switch {
	case err != nil:
		fmt.Fprintf(&report, "Failed to restore cache: %s", logValue(err.Error()))
	case result.CacheRestored:
		match := "exact"
		if result.FallbackUsed {
			match = "fallback"
		}
		fmt.Fprintf(&report, "Cache restored using %s key %s from %s", match, logValue(result.Key), displayScopes(result.Scopes))
	case result.NotRestoredReason != "":
		report.WriteString(logValue(result.NotRestoredReason))
	case incomplete:
		report.WriteString("Cache not restored: search incomplete")
	case denied:
		report.WriteString("Cache not restored: no allowed entry found (registry policy denied matching entries)")
	case result.Diagnostics == nil:
		report.WriteString("Cache not restored (search diagnostics unavailable)")
	default:
		report.WriteString("Cache miss")
	}
	return report.String()
}

func displayScopes(scopes map[string]string) string {
	var parts []string
	for _, name := range []string{"pipeline", "branch", "build"} {
		if value, ok := scopes[name]; ok {
			parts = append(parts, name+"="+logValue(value))
		}
	}
	if len(parts) == 0 {
		return "unscoped"
	}
	return strings.Join(parts, ", ")
}

func logValue(value string) string {
	quoted := strconv.QuoteToGraphic(value)
	return quoted[1 : len(quoted)-1]
}
