package terminal

import (
	"errors"
	"io"
	"net"
	"time"
)

// Session is a live terminal connection (SSH or Telnet).
type Session interface {
	io.Reader
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

// ErrLost means the connection broke instead of ending normally
// (e.g. the shell exited); the frontend may reconnect automatically.
var ErrLost = errors.New("네트워크 연결이 끊어졌습니다")

// ErrNoReply is a keepalive that got no answer in time.
var ErrNoReply = errors.New("응답이 없습니다")

// KeepAliveTimeout is how long one keepalive may wait for an answer.
var KeepAliveTimeout = 15 * time.Second

func IsTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// ConnectRequest carries the values from the Connect dialog.
type ConnectRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Pass     string `json:"pass"`
	Encoding string `json:"encoding"`
	Jump     string `json:"jump"` // SSH jump hosts (ssh -J); "" = ProxyJump of ~/.ssh/config, "none" = none
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// Protocol returns the protocol of a well-known port ("ssh" 22, "ftp" 21,
// "telnet" 23), or "" when it must be detected from the server greeting.
func Protocol(port int) string {
	switch port {
	case 22:
		return "ssh"
	case 21:
		return "ftp"
	case 23:
		return "telnet"
	}
	return ""
}

// PromptField is one input in a prompt: a label and whether to hide what is typed.
type PromptField struct {
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
}

// Asker asks the user for the fields' values; ok is false if they cancelled.
type Asker func(title, message string, fields []PromptField) (values []string, ok bool)
