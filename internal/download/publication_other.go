//go:build !darwin && !linux

package download

import "errors"

func renameExclusive(string, string) error {
	return errors.ErrUnsupported
}
