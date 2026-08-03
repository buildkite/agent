package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func TestTruncateEnv(t *testing.T) {
	l := logger.NewBuffer()
	key := "FOO"
	env := map[string]string{key: strings.Repeat("a", 100)}
	limit := 64
	if err := truncateEnv(l, env, key, limit); err != nil {
		t.Fatalf("truncateEnv(logger, %v, %q, %d) = %v", env, key, limit, err)
	}
	if got, want := env["FOO"], "aaaaaaaaaaaaaaaaaaaaaaaaaa[value truncated 100 -> 59 bytes]"; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): env[%q] = %q, want %q", env, key, limit, key, got, want)
	}
	format := "FOO=%s\000"
	if got, want := len(fmt.Sprintf(format, env["FOO"])), limit; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): len(fmt.Sprintf(%q, env[%q])) = %d, want %d", env, key, limit, format, key, got, want)
	}
}

func TestValidateJobValue(t *testing.T) {
	bkTarget := "github.com/buildkite/test"
	bkTargetRE := regexp.MustCompile(`^github\.com/buildkite/.*`)
	ghTargetRE := regexp.MustCompile(`^github\.com/nope/.*`)

	tests := []struct {
		name           string
		allowedTargets []*regexp.Regexp
		pipelineTarget string
		wantErr        bool
	}{
		{
			name:           "No error. Allowed targets no configured.",
			allowedTargets: []*regexp.Regexp{},
			pipelineTarget: bkTarget,
		}, {
			name:           "No pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE},
			pipelineTarget: bkTarget,
			wantErr:        true,
		}, {
			name:           "Pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE, bkTargetRE},
			pipelineTarget: bkTarget,
		},
	}

	for _, tc := range tests {
		err := validateJobValue(tc.allowedTargets, tc.pipelineTarget)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateJobValue() error = %v, wantErr = %v", err, tc.wantErr)
		}
	}
}

func TestJobTimeoutFilePath(t *testing.T) {
	t.Parallel()

	got := jobTimeoutFilePath("abc123", jobContextDir(JobRunnerConfig{}))
	want := filepath.Join(os.TempDir(), "job-timeout-abc123")
	if got != want {
		t.Errorf("jobTimeoutFilePath(%q, jobContextDir({})) = %q, want %q", "abc123", got, want)
	}

	k8sDir := jobContextDir(JobRunnerConfig{KubernetesExec: true})
	if got, want := jobTimeoutFilePath("abc123", k8sDir), filepath.Join("/workspace", "job-timeout-abc123"); got != want {
		t.Errorf("jobTimeoutFilePath(%q, %q) = %q, want %q", "abc123", k8sDir, got, want)
	}
}

func TestJobContextDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		conf JobRunnerConfig
		want string
	}{
		{
			name: "default",
			conf: JobRunnerConfig{},
			want: os.TempDir(),
		},
		{
			name: "explicit_dir",
			conf: JobRunnerConfig{JobContextDir: "/var/lib/buildkite/job"},
			want: "/var/lib/buildkite/job",
		},
		{
			name: "kubernetes_default",
			conf: JobRunnerConfig{KubernetesExec: true},
			want: "/workspace",
		},
		{
			name: "kubernetes_explicit_dir",
			conf: JobRunnerConfig{
				KubernetesExec: true,
				JobContextDir:  "/buildkite-shared",
			},
			want: "/buildkite-shared",
		},
	}

	for _, tc := range tests {
		if got := jobContextDir(tc.conf); got != tc.want {
			t.Errorf("%s: jobContextDir(%+v) = %q, want %q", tc.name, tc.conf, got, tc.want)
		}
	}
}

func TestCancelReasonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason CancelReason
		want   string
	}{
		{CancelReasonJobState, "job cancelled on Buildkite"},
		{CancelReasonAgentStopping, "agent is stopping"},
		{CancelReasonInvalidToken, "access token is invalid"},
		{CancelReasonJobTimeout, "job timed out on Buildkite"},
		{CancelReason(99), "unknown"},
	}

	for _, tc := range tests {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("CancelReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestTimestampLine(t *testing.T) {
	t.Parallel()

	const ts = "2026-07-31T15:56:23Z"

	tests := []struct {
		name, line, want string
	}{
		{
			name: "plain line",
			line: "Analyzing targets",
			want: "[2026-07-31T15:56:23Z] Analyzing targets",
		},
		{
			name: "empty line",
			line: "",
			want: "[2026-07-31T15:56:23Z] ",
		},
		{
			// Bazel and friends redraw progress by moving the cursor up and
			// erasing. The timestamp has to land after the movement, or it ends
			// up on a different row to the text it belongs to.
			name: "cursor movement is kept in front of the timestamp",
			line: "\r\x1b[1A\x1b[K(15:56:23) Computing main repo mapping:",
			want: "\r\x1b[1A\x1b[K[2026-07-31T15:56:23Z] (15:56:23) Computing main repo mapping:",
		},
		{
			name: "repeated cursor movement",
			line: "\r\x1b[1A\x1b[K\r\x1b[1A\x1b[K(15:56:23) ERROR: Results may be incomplete",
			want: "\r\x1b[1A\x1b[K\r\x1b[1A\x1b[K[2026-07-31T15:56:23Z] (15:56:23) ERROR: Results may be incomplete",
		},
		{
			// Timestamping these writes a visible row where the process wrote
			// none, which is what leaves stray timestamps behind in the log.
			name: "control sequences only",
			line: "\r\x1b[1A\x1b[K",
			want: "\r\x1b[1A\x1b[K",
		},
		{
			name: "carriage return only",
			line: "\r",
			want: "\r",
		},
		{
			// ESC M (reverse index) moves up a row without a CSI, so it strands
			// the timestamp the same way ESC [ 1 A does.
			name: "reverse index",
			line: "\x1bM\x1b[K(15:56:23) Building:",
			want: "\x1bM\x1b[K[2026-07-31T15:56:23Z] (15:56:23) Building:",
		},
		{
			name: "save and restore cursor position",
			line: "\x1b7\x1b8\x1b[Kanalyzing",
			want: "\x1b7\x1b8\x1b[K[2026-07-31T15:56:23Z] analyzing",
		},
		{
			// An erase would wipe a timestamp written in front of it.
			name: "erase line",
			line: "\x1b[2Kfetching",
			want: "\x1b[2K[2026-07-31T15:56:23Z] fetching",
		},
		{
			// Colour codes move nothing, so the timestamp stays where it has
			// always been. Stepping over them only paints it in the line's
			// colour.
			name: "colour code",
			line: "\x1b[0mno actions running",
			want: "[2026-07-31T15:56:23Z] \x1b[0mno actions running",
		},
		{
			name: "colour code in front of movement",
			line: "\x1b[32m\r\x1b[1A\x1b[Kcompiling",
			want: "\x1b[32m\r\x1b[1A\x1b[K[2026-07-31T15:56:23Z] compiling",
		},
		{
			name: "escape sequence later in the line is left alone",
			line: "INFO: \x1b[32mBuild completed\x1b[0m",
			want: "[2026-07-31T15:56:23Z] INFO: \x1b[32mBuild completed\x1b[0m",
		},
		{
			// The renderer draws these as literal text rather than acting on
			// them, so they can't displace anything: ESC E prints "E", and a
			// colon subparameter or a final outside its set aborts the whole
			// sequence back to visible characters.
			name: "next line",
			line: "\x1bEfetching",
			want: "[2026-07-31T15:56:23Z] \x1bEfetching",
		},
		{
			// Because the renderer draws it, ESC E also ends the run rather than
			// counting towards it. Otherwise this line looks like one the process
			// used only to move the cursor, and loses the timestamp for the "E"
			// the renderer does put on the row.
			name: "text-drawn escape then a carriage return",
			line: "\x1bE\r",
			want: "[2026-07-31T15:56:23Z] \x1bE\r",
		},
		{
			// Known limitation: an unrecognised final aborts the sequence back to
			// visible text too, but spotting that means knowing which finals the
			// renderer acts on. The line renders "[?25P" with no timestamp.
			name: "aborted sequence then a carriage return",
			line: "\x1b[?25P\r",
			want: "\x1b[?25P\r",
		},
		{
			name: "subparameter colour codes",
			line: "\x1b[38:2:255:0:0mRED",
			want: "[2026-07-31T15:56:23Z] \x1b[38:2:255:0:0mRED",
		},
		{
			name: "horizontal position absolute",
			line: "\x1b[3`overwrite",
			want: "[2026-07-31T15:56:23Z] \x1b[3`overwrite",
		},
		{
			// The renderer ignores private sequences, so hiding the cursor
			// leaves the timestamp alone.
			name: "hidden cursor",
			line: "\x1b[?25lAnalyzing targets",
			want: "[2026-07-31T15:56:23Z] \x1b[?25lAnalyzing targets",
		},
		{
			name: "backspace",
			line: "\bretry",
			want: "\b[2026-07-31T15:56:23Z] retry",
		},
		{
			// Moving right leaves a timestamp written at the start of the line
			// alone, so it keeps column zero and stays aligned with every other
			// line's. Moving left or up doesn't, hence the two below.
			name: "cursor forward",
			line: "\x1b[5Cfetching",
			want: "[2026-07-31T15:56:23Z] \x1b[5Cfetching",
		},
		{
			name: "cursor back",
			line: "\x1b[5Dfetching",
			want: "\x1b[5D[2026-07-31T15:56:23Z] fetching",
		},
		{
			name: "cursor forward then up",
			line: "\x1b[5C\x1b[1Afetching",
			want: "\x1b[5C\x1b[1A[2026-07-31T15:56:23Z] fetching",
		},
		{
			// A progress bar redrawing with carriage returns and no newline
			// arrives as one line. Only what follows the last redraw is left on
			// the row, so a timestamp anywhere in front of it is written over.
			name: "redrawn within the line",
			line: "downloading 10%\rdownloading 50%",
			want: "downloading 10%\r[2026-07-31T15:56:23Z] downloading 50%",
		},
		{
			name: "redrawn repeatedly within the line",
			line: "a\rbb\rccc",
			want: "a\rbb\r[2026-07-31T15:56:23Z] ccc",
		},
		{
			name: "redrawn behind an erase",
			line: "downloading 10%\r\x1b[Kdownloading 50%",
			want: "downloading 10%\r\x1b[K[2026-07-31T15:56:23Z] downloading 50%",
		},
		{
			// Nothing is redrawn after these, so the row keeps what came before
			// and the timestamp belongs in front of it, as it always has.
			name: "trailing carriage return",
			line: "fetching\r",
			want: "[2026-07-31T15:56:23Z] fetching\r",
		},
		{
			name: "carriage return then movement only",
			line: "fetching\r\x1b[1A",
			want: "[2026-07-31T15:56:23Z] fetching\r\x1b[1A",
		},
		{
			// A carriage return inside the leading run is already accounted for.
			name: "carriage return within the leading run",
			line: "\r\x1b[K\rfetching",
			want: "\r\x1b[K\r[2026-07-31T15:56:23Z] fetching",
		},
		{
			// Known limitation: CSI G reaches column zero on the same row just as
			// a carriage return does, so what follows it writes over a timestamp
			// at the start of the line. Placing the timestamp after it means
			// knowing the column the sequence addresses, which means modelling the
			// renderer. Carriage returns are how progress bars redraw in practice.
			name: "column absolute within the line is not anchored on",
			line: "Analyzing\x1b[Gdone",
			want: "[2026-07-31T15:56:23Z] Analyzing\x1b[Gdone",
		},
		{
			name: "column one absolute within the line is not anchored on",
			line: "Analyzing\x1b[1Gdone",
			want: "[2026-07-31T15:56:23Z] Analyzing\x1b[1Gdone",
		},
		{
			// CSI E reaches column zero on another row, where a timestamp at the
			// start of the line was never in the way.
			name: "next line within the line",
			line: "step one\x1b[Estep two",
			want: "[2026-07-31T15:56:23Z] step one\x1b[Estep two",
		},
		{
			// An OSC draws nothing and moves nothing, so the timestamp stays at
			// the start of the line. Matching it only matters for finding where
			// the run ends, as the next case shows.
			name: "window title",
			line: "\x1b]0;bazel build\x07Analyzing targets",
			want: "[2026-07-31T15:56:23Z] \x1b]0;bazel build\x07Analyzing targets",
		},
		{
			name: "window title in front of movement",
			line: "\x1b]0;bazel build\x07\x1b[1A\x1b[Kcompiling",
			want: "\x1b]0;bazel build\x07\x1b[1A\x1b[K[2026-07-31T15:56:23Z] compiling",
		},
		{
			name: "string terminator ends the window title",
			line: "\x1b]0;bazel build\x1b\\\x1b[1Acompiling",
			want: "\x1b]0;bazel build\x1b\\\x1b[1A[2026-07-31T15:56:23Z] compiling",
		},
		{
			// An image is drawn on a line of its own, so a timestamp in front of
			// it is left alone on the line above.
			name: "external image",
			line: "\x1b]1338;url=x\x07uploaded",
			want: "\x1b]1338;url=x\x07[2026-07-31T15:56:23Z] uploaded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := timestampLine(tc.line, ts); got != tc.want {
				t.Errorf("timestampLine(%q, %q) = %q, want %q", tc.line, ts, got, tc.want)
			}
		})
	}
}
