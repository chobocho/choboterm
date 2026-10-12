//go:build termux

package serial

import (
	"errors"
	"io"
)

const Supported = false

var errTermuxSerialUnsupported = errors.New("serial ports are not supported in the Termux build")

// IsPort reports false because the Termux build does not provide serial ports.
func IsPort(string) bool { return false }

func Open(serialConfig) (*Session, error) { return nil, errTermuxSerialUnsupported }

func (s *Session) Read(p []byte) (int, error) {
	if s == nil || s.f == nil {
		return 0, io.EOF
	}
	return s.f.Read(p)
}

func (s *Session) SendBreak() error { return errTermuxSerialUnsupported }

func ListPorts() []SerialPort { return nil }
