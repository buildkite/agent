//go:build windows

package system

import (
	"fmt"

	"github.com/buildkite/agent/v3/logger"
	"golang.org/x/sys/windows"
)

var (
	VER_NT_WORKSTATION       = byte(0x0000001)
	VER_NT_DOMAIN_CONTROLLER = byte(0x0000002)
	VER_NT_SERVER            = byte(0x0000003)
)

func VersionDump(_ logger.Logger) (string, error) {
	info := windows.RtlGetVersion()

	return fmt.Sprintf("Windows version %d.%d (Build %d) (ProductType %s)\n", info.MajorVersion, info.MinorVersion, info.BuildNumber, productTypeToString(info.ProductType)), nil
}

func productTypeToString(productType byte) string {
	switch productType {
	case VER_NT_WORKSTATION:
		return "VER_NT_WORKSTATION"
	case VER_NT_DOMAIN_CONTROLLER:
		return "VER_NT_DOMAIN_CONTROLLER"
	case VER_NT_SERVER:
		return "VER_NT_SERVER"
	default:
		return "UNKNOWN"
	}
}
