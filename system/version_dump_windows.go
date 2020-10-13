package system

import (
	"fmt"
	"github.com/buildkite/agent/v3/logger"
	"golang.org/x/sys/windows"
)

func VersionDump(_ logger.Logger) (string, error) {
	info := windows.RtlGetVersion()

	return fmt.Sprintf("Windows version %d.%d (Build %d)\n", info.MajorVersion, info.MinorVersion, info.BuildNumber), nil
}
