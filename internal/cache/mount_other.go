//go:build !linux

package cache

func cleanupMount(string) (bool, error) { return false, nil }
