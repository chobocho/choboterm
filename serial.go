package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// A serial port opens in a tab like a connection: the Host field names the
// port ("COM3", "/dev/ttyUSB0") and the Port field holds the speed (baud).
// The Host field may also carry the speed and line settings after the name:
// "COM3 9600", "COM3 9600 7E1 rtscts", "/dev/ttyUSB0 115200 8N1 xonxoff".

// SerialPort is a serial port of this PC for the Host list.
type SerialPort struct {
	Name  string `json:"name"`  // what goes in the Host field: "COM3", "/dev/ttyUSB0"
	Label string `json:"label"` // for the list: "USB-SERIAL CH340"...
}

// defaultBaud is the speed used when neither field gives one.
const defaultBaud = 115200

// serialConfig is how a serial port is opened.
type serialConfig struct {
	Name     string // "COM3", "/dev/ttyUSB0"
	Baud     int
	DataBits int    // 5..8
	Parity   byte   // 'N', 'E', 'O'
	StopBits int    // 1 or 2
	Flow     string // "none", "rtscts", "xonxoff"
}

// String is how the settings read in the tab title and the log: "COM3 115200 8N1".
func (c serialConfig) String() string {
	s := fmt.Sprintf("%s %d %d%c%d", c.Name, c.Baud, c.DataBits, c.Parity, c.StopBits)
	if c.Flow != "none" {
		s += " " + c.Flow
	}
	return s
}

var (
	serialName  = regexp.MustCompile(`(?i)^(com[1-9][0-9]*|/dev/\S+)$`)
	serialFrame = regexp.MustCompile(`(?i)^([5-8])([neo])([12])$`)
)

// isSerial reports whether the Host field names a serial port.
func isSerial(host string) bool {
	name, _, _ := strings.Cut(strings.TrimSpace(host), " ")
	return serialName.MatchString(name)
}

// parseSerial reads the Host field of a serial port; port is the Port field,
// used as the speed when the Host field gives none (the usual 21/22/23 of a
// server mean nothing here and give the default speed).
func parseSerial(host string, port int) (serialConfig, error) {
	f := strings.Fields(host)
	if len(f) == 0 || !serialName.MatchString(f[0]) {
		return serialConfig{}, fmt.Errorf("시리얼 포트 이름이 올바르지 않습니다: %s", host)
	}
	c := serialConfig{Name: f[0], Baud: port, DataBits: 8, Parity: 'N', StopBits: 1, Flow: "none"}
	if strings.HasPrefix(strings.ToLower(c.Name), "com") {
		c.Name = strings.ToUpper(c.Name)
	}
	if c.Baud == 0 || Protocol(c.Baud) != "" {
		c.Baud = defaultBaud
	}
	for _, w := range f[1:] {
		lw := strings.ToLower(w)
		if m := serialFrame.FindStringSubmatch(w); m != nil {
			c.DataBits, _ = strconv.Atoi(m[1])
			c.Parity = strings.ToUpper(m[2])[0]
			c.StopBits, _ = strconv.Atoi(m[3])
		} else if n, err := strconv.Atoi(w); err == nil {
			c.Baud = n
		} else if lw == "rtscts" || lw == "xonxoff" || lw == "none" {
			c.Flow = lw
		} else {
			return serialConfig{}, fmt.Errorf("알 수 없는 시리얼 설정: %s (예: COM3 9600 8N1 rtscts)", w)
		}
	}
	if c.Baud < 50 || c.Baud > 4000000 {
		return serialConfig{}, fmt.Errorf("속도(baud)가 올바르지 않습니다: %d", c.Baud)
	}
	return c, nil
}

// errSerialGone is a port whose device went away (a USB adapter unplugged).
var errSerialGone = errors.New("시리얼 장치가 분리되었습니다")

// serialSession is an open serial port. There is no window size to tell the
// other end, and no keepalive: a device may stay silent for good.
type serialSession struct {
	f      *os.File
	closed atomic.Bool
}

func (s *serialSession) Write(p []byte) (int, error) { return s.f.Write(p) }

func (s *serialSession) Resize(cols, rows int) error { return nil }

func (s *serialSession) Close() error {
	s.closed.Store(true)
	return s.f.Close()
}

// connectSerial opens a serial port in the tab.
func (a *App) connectSerial(tabID int, req ConnectRequest) (string, error) {
	c, err := parseSerial(req.Host, req.Port)
	if err != nil {
		return "", err
	}
	t := a.getTab(tabID)
	t.disconnect()
	sess, err := openSerial(c)
	if err != nil {
		return "", err
	}
	enc := t.codec.Set(req.Encoding)
	req.Port = c.Baud
	_ = addHistory(HostEntry{Host: req.Host, Port: c.Baud, Encoding: enc}, nil)
	t.start(sess, "serial", req)
	return "serial", nil
}

// GetSerialPorts lists the serial ports of this PC.
func (a *App) GetSerialPorts() []SerialPort {
	return listSerialPorts()
}

// SendBreak sends a break signal on a tab's serial port (e.g. to enter a
// router's ROM monitor).
func (a *App) SendBreak(tabID int) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	s, ok := t.session().(*serialSession)
	if !ok {
		return errors.New("시리얼 포트에서만 Break를 보낼 수 있습니다")
	}
	t.log.mark("Break 신호를 보냈습니다")
	return s.sendBreak()
}
