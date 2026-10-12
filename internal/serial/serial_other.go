//go:build !windows && !linux

package serial

import "errors"

func Open(c serialConfig) (*Session, error) {
	return nil, errors.New("이 운영체제에서는 시리얼 포트를 쓸 수 없습니다")
}

func (s *Session) Read(p []byte) (int, error) { return s.f.Read(p) }

func (s *Session) SendBreak() error {
	return errors.New("이 운영체제에서는 Break를 보낼 수 없습니다")
}

func ListPorts() []SerialPort { return nil }
