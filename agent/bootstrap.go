package agent

import (
	"fmt"
	"os"
	"strings"
)

type Bootstrap struct {
	Debug bool
}

func (b Bootstrap) Run() error {
	// Show BUILDKITE_* environment variables if in debug mode
	if b.Debug {
		fmt.Println("~~~ Build environment variables")
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "BUILDKITE") {
				fmt.Println(e)
			}
		}
	}

	fmt.Printf("Lollies")

	return nil
}
