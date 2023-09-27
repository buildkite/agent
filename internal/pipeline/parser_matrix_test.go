package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/google/go-cmp/cmp"
)

func TestParser_Matrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc     string
		input    string
		want     *Pipeline
		wantJSON string
	}{
		{
			desc: "Single anonymous dimension at top level",
			input: `---
steps:
  - command: echo {{matrix}}
    matrix:
      - apple
      - 47
      - true
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "echo {{matrix}}",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"": {"apple", 47, true},
							},
						},
					},
				},
			},
			wantJSON: `{
  "steps": [
    {
      "command": "echo {{matrix}}",
      "matrix": [
        "apple",
        47,
        true
      ]
    }
  ]
}`,
		},
		{
			desc: "Single anonymous dimension in setup",
			input: `---
steps:
  - command: echo {{matrix}}
    matrix:
      setup:
        - apple
        - true
        - 47
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "echo {{matrix}}",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"": {"apple", true, 47},
							},
						},
					},
				},
			},
			wantJSON: `{
  "steps": [
    {
      "command": "echo {{matrix}}",
      "matrix": [
        "apple",
        true,
        47
      ]
    }
  ]
}`,
		},
		{
			desc: "Single anonymous dimension in setup with adjustments",
			input: `---
steps:
  - command: echo {{matrix}}
    matrix:
      setup:
        - apple
        - 47
        - true
      adjustments:
        - with: orange
          skip: true
        - with: 42
          soft_fail: true
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "echo {{matrix}}",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"": {"apple", 47, true},
							},
							Adjustments: MatrixAdjustments{
								{
									With: MatrixAdjustmentWith{"": "orange"},
									Skip: true,
								},
								{
									With:     MatrixAdjustmentWith{"": 42},
									SoftFail: true,
								},
							},
						},
					},
				},
			},
			wantJSON: `{
  "steps": [
    {
      "command": "echo {{matrix}}",
      "matrix": {
        "adjustments": [
          {
            "skip": true,
            "with": "orange"
          },
          {
            "soft_fail": true,
            "with": 42
          }
        ],
        "setup": [
          "apple",
          47,
          true
        ]
      }
    }
  ]
}`,
		},
		{
			desc: "Multiple dimensions",
			input: `---
steps:
  - command: GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build
    matrix:
      setup:
        os:
          - windows
          - linux
          - darwin
        arch:
          - arm64
          - amd64
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"arch": {"arm64", "amd64"},
								"os":   {"windows", "linux", "darwin"},
							},
						},
					},
				},
			},
			wantJSON: `{
  "steps": [
    {
      "command": "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
      "matrix": {
        "setup": {
          "arch": [
            "arm64",
            "amd64"
          ],
          "os": [
            "windows",
            "linux",
            "darwin"
          ]
        }
      }
    }
  ]
}`,
		},
		{
			desc: "Multiple dimensions and adjustments",
			input: `---
steps:
  - command: GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build
    matrix:
      setup:
        os:
          - windows
          - linux
          - darwin
        arch:
          - arm64
          - amd64
      adjustments:
        - with:
            os: plan9
            arch: mips
        - with:
            os: nextstep
            arch: m68k
        - with:
            os: windows
            arch: arm64
          skip: true
        - with:
            os: 8
            arch: ppc
          soft_fail: true
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"arch": {"arm64", "amd64"},
								"os":   {"windows", "linux", "darwin"},
							},
							Adjustments: MatrixAdjustments{
								{
									With: MatrixAdjustmentWith{
										"arch": "mips",
										"os":   "plan9",
									},
								},
								{
									With: MatrixAdjustmentWith{
										"arch": "m68k",
										"os":   "nextstep",
									},
								},
								{
									With: MatrixAdjustmentWith{
										"arch": "arm64",
										"os":   "windows",
									},
									Skip: true,
								},
								{
									With: MatrixAdjustmentWith{
										"arch": "ppc",
										"os":   8,
									},
									SoftFail: true,
								},
							},
						},
					},
				},
			},
			wantJSON: `{
  "steps": [
    {
      "command": "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
      "matrix": {
        "adjustments": [
          {
            "with": {
              "arch": "mips",
              "os": "plan9"
            }
          },
          {
            "with": {
              "arch": "m68k",
              "os": "nextstep"
            }
          },
          {
            "skip": true,
            "with": {
              "arch": "arm64",
              "os": "windows"
            }
          },
          {
            "soft_fail": true,
            "with": {
              "arch": "ppc",
              "os": 8
            }
          }
        ],
        "setup": {
          "arch": [
            "arm64",
            "amd64"
          ],
          "os": [
            "windows",
            "linux",
            "darwin"
          ]
        }
      }
    }
  ]
}`,
		},
		{
			desc: "Multiple dimensions, adjustments, and interpolation",
			input: `---
env:
  OS_KEY: os
  FAVOURITE_OS: darwin
  FAVOURITE_ARCH: arm64
  BIG_IRON_OS: zos
  BIG_IRON_ARCH: s390x
steps:
  - command: GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build
    matrix:
      setup:
        "${OS_KEY}":
          - windows
          - linux
          - ${FAVOURITE_OS}
        arch:
          - ${FAVOURITE_ARCH}
          - amd64
      adjustments:
        - with:
            "${OS_KEY}": ${BIG_IRON_OS}
            arch: ${BIG_IRON_ARCH}
          soft_fail: true
`,
			want: &Pipeline{
				Env: ordered.MapFromItems(
					ordered.TupleSS{Key: "OS_KEY", Value: "os"},
					ordered.TupleSS{Key: "FAVOURITE_OS", Value: "darwin"},
					ordered.TupleSS{Key: "FAVOURITE_ARCH", Value: "arm64"},
					ordered.TupleSS{Key: "BIG_IRON_OS", Value: "zos"},
					ordered.TupleSS{Key: "BIG_IRON_ARCH", Value: "s390x"},
				),
				Steps: Steps{
					&CommandStep{
						Command: "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
						Matrix: &Matrix{
							Setup: MatrixSetup{
								"arch": {"arm64", "amd64"},
								"os":   {"windows", "linux", "darwin"},
							},
							Adjustments: MatrixAdjustments{
								{
									With: MatrixAdjustmentWith{
										"arch": "s390x",
										"os":   "zos",
									},
									SoftFail: true,
								},
							},
						},
					},
				},
			},
			wantJSON: `{
  "env": {
    "OS_KEY": "os",
    "FAVOURITE_OS": "darwin",
    "FAVOURITE_ARCH": "arm64",
    "BIG_IRON_OS": "zos",
    "BIG_IRON_ARCH": "s390x"
  },
  "steps": [
    {
      "command": "GOOS={{matrix.os}} GOARCH={{matrix.arch}} go build",
      "matrix": {
        "adjustments": [
          {
            "soft_fail": true,
            "with": {
              "arch": "s390x",
              "os": "zos"
            }
          }
        ],
        "setup": {
          "arch": [
            "arm64",
            "amd64"
          ],
          "os": [
            "windows",
            "linux",
            "darwin"
          ]
        }
      }
    }
  ]
}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", test.input, err)
			}
			if err := got.Interpolate(nil); err != nil {
				t.Fatalf("Pipeline.Interpolate(nil) = %v", err)
			}
			if diff := cmp.Diff(got, test.want, cmp.Comparer(ordered.EqualSS)); diff != "" {
				t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
			}
			gotJSON, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
			}
			if diff := cmp.Diff(string(gotJSON), test.wantJSON); diff != "" {
				t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
			}
		})
	}
}
