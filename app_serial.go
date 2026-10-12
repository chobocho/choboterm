package main

import (
	"errors"

	"choboterm/internal/serial"
	"choboterm/internal/store"
	"choboterm/internal/terminal"
)

// connectSerial opens a serial port in the tab.
func (a *App) connectSerial(tabID int, req terminal.ConnectRequest) (string, error) {
	c, err := serial.Parse(req.Host, req.Port)
	if err != nil {
		return "", err
	}
	t := a.getTab(tabID)
	t.disconnect()
	sess, err := serial.Open(c)
	if err != nil {
		return "", err
	}
	enc := t.codec.Set(req.Encoding)
	req.Port = c.Baud
	_ = store.AddHistory(store.HostEntry{Host: req.Host, Port: c.Baud, Encoding: enc}, nil)
	t.start(sess, "serial", req)
	return "serial", nil
}

// GetSerialPorts lists the serial ports of this PC.
func (a *App) GetSerialPorts() []serial.SerialPort {
	return serial.ListPorts()
}

// SendBreak sends a break signal on a tab's serial port (e.g. to enter a
// router's ROM monitor).
func (a *App) SendBreak(tabID int) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	s, ok := t.session().(*serial.Session)
	if !ok {
		return errors.New("시리얼 포트에서만 Break를 보낼 수 있습니다")
	}
	t.log.Mark("Break 신호를 보냈습니다")
	return s.SendBreak()
}
