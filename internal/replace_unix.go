//go:build !windows

package internal

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
