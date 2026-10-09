//go:build !windows && !linux

package main

import "errors"

func openSerial(c serialConfig) (*serialSession, error) {
	return nil, errors.New("이 운영체제에서는 시리얼 포트를 쓸 수 없습니다")
}

func (s *serialSession) Read(p []byte) (int, error) { return s.f.Read(p) }

func (s *serialSession) sendBreak() error {
	return errors.New("이 운영체제에서는 Break를 보낼 수 없습니다")
}

func listSerialPorts() []SerialPort { return nil }
