//go:build !windows && !linux

package main

import "errors"

func installShortcut() (string, error) {
	return "", errors.New("shortcut install is only wired up for Windows and Linux")
}

func removeShortcut() error {
	return errors.New("shortcut removal is only wired up for Windows and Linux")
}
