//go:build termux

package main

import (
	"errors"
	"io"
)

const serialSupported = false

var errTermuxSerialUnsupported = errors.New("serial ports are not supported in the Termux build")

// isSerial reports false because the Termux build does not provide serial ports.
func isSerial(string) bool { return false }

func openSerial(serialConfig) (*serialSession, error) { return nil, errTermuxSerialUnsupported }

func (s *serialSession) Read(p []byte) (int, error) {
	if s == nil || s.f == nil {
		return 0, io.EOF
	}
	return s.f.Read(p)
}

func (s *serialSession) sendBreak() error { return errTermuxSerialUnsupported }

func listSerialPorts() []SerialPort { return nil }
