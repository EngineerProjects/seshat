//go:build !(linux || darwin || windows) || arm || 386 || ios || android

package fsext

import "errors"

func ReadClipboardText() (string, error) {
	return "", errors.New("clipboard not supported on this platform")
}
