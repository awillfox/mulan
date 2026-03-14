//go:build !windows

package cashdrawer

import "errors"

func Init(_ string) {}

func Open() error {
	return errors.New("cash drawer I/O port control is only supported on Windows")
}
