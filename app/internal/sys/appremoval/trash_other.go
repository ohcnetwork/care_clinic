//go:build !darwin

package appremoval

import "errors"

func trash(string) error {
	return errors.New("moving the app to the Trash is only supported on macOS")
}
