//go:build (linux || darwin || windows) && !arm && !386 && !ios && !android

package fsext

import "github.com/aymanbagabas/go-nativeclipboard"

// ReadClipboardText returns the current text content of the system clipboard.
// Uses native OS APIs (Wayland, X11, Cocoa, Win32) rather than spawning
// external tools, so it works without xclip / wl-paste installed.
func ReadClipboardText() (string, error) {
	b, err := nativeclipboard.Text.Read()
	return string(b), err
}
