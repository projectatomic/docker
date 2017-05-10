// +build !linux !cgo static_build !journald

package journald

func (s *journald) Close() error {
	s.closeWriter()
	return nil
}
