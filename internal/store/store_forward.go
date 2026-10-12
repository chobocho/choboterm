package store

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Forward is one SSH port forwarding rule.
//
//	L: this PC listens on Bind, and connections go from the server to Host:Port (ssh -L).
//	R: the server listens on Bind, and connections go from this PC to Host:Port (ssh -R).
//	D: this PC listens on Bind as a SOCKS5 proxy that connects from the server (ssh -D).
type Forward struct {
	Type     string `json:"type"`
	BindAddr string `json:"bindAddr"`
	BindPort int    `json:"bindPort"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// Normalize checks f and fills in default bind addresses.
func (f Forward) Normalize() (Forward, error) {
	switch f.Type {
	case "L", "D":
		if f.BindAddr == "" {
			f.BindAddr = "127.0.0.1"
		}
	case "R":
		if f.BindAddr == "" {
			f.BindAddr = "localhost"
		}
	default:
		return f, fmt.Errorf("알 수 없는 포워딩 종류: %q", f.Type)
	}
	if f.BindPort <= 0 || f.BindPort > 65535 {
		return f, errors.New("수신 포트가 올바르지 않습니다")
	}
	if f.Type == "D" {
		f.Host, f.Port = "", 0
		return f, nil
	}
	if f.Host == "" {
		return f, errors.New("대상 호스트를 입력하세요")
	}
	if f.Port <= 0 || f.Port > 65535 {
		return f, errors.New("대상 포트가 올바르지 않습니다")
	}
	return f, nil
}

func (f Forward) Bind() string { return net.JoinHostPort(f.BindAddr, strconv.Itoa(f.BindPort)) }

func (f Forward) Target() string { return net.JoinHostPort(f.Host, strconv.Itoa(f.Port)) }

func (f Forward) Same(o Forward) bool {
	return f.Type == o.Type && f.BindAddr == o.BindAddr && f.BindPort == o.BindPort
}
