//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// DCB.Flags bits (winbase.h).
const (
	dcbBinary        = 0x0001
	dcbParity        = 0x0002
	dcbOutxCtsFlow   = 0x0004
	dcbDtrEnable     = 0x0010 // fDtrControl = DTR_CONTROL_ENABLE
	dcbOutX          = 0x0100
	dcbInX           = 0x0200
	dcbRtsEnable     = 0x1000 // fRtsControl = RTS_CONTROL_ENABLE
	dcbRtsHandshake  = 0x2000 // fRtsControl = RTS_CONTROL_HANDSHAKE
	dcbAbortOnError  = 0x4000
	dcbTXContOnXoff  = 0x0080
	dcbControlFields = 0x7fff
)

// openSerial opens and sets up a serial port. The handle is opened for
// overlapped I/O, so os.File reads and writes run at the same time and Close
// ends a read that is waiting for data.
func openSerial(c serialConfig) (*serialSession, error) {
	path, err := windows.UTF16PtrFromString(`\\.\` + c.Name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		switch {
		case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
			return nil, fmt.Errorf("%s 포트가 없습니다. 장치가 연결되어 있는지 확인하세요", c.Name)
		case errors.Is(err, windows.ERROR_ACCESS_DENIED), errors.Is(err, windows.ERROR_SHARING_VIOLATION):
			return nil, fmt.Errorf("%s 포트를 다른 프로그램(또는 다른 탭)이 쓰고 있습니다", c.Name)
		}
		return nil, fmt.Errorf("%s 포트를 열 수 없습니다: %w", c.Name, err)
	}
	if err := setupSerial(h, c); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("%s 포트를 설정할 수 없습니다: %w", c.Name, err)
	}
	return &serialSession{f: os.NewFile(uintptr(h), c.Name)}, nil
}

func setupSerial(h windows.Handle, c serialConfig) error {
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		return err
	}
	dcb.BaudRate = uint32(c.Baud)
	dcb.ByteSize = uint8(c.DataBits)
	dcb.Parity = map[byte]uint8{'N': 0, 'O': 1, 'E': 2}[c.Parity] // NOPARITY, ODDPARITY, EVENPARITY
	dcb.StopBits = 0                                              // ONESTOPBIT
	if c.StopBits == 2 {
		dcb.StopBits = 2 // TWOSTOPBITS
	}
	// DTR and RTS stay on: many devices only talk to a port that has them.
	flags := uint32(dcbBinary | dcbDtrEnable | dcbTXContOnXoff)
	if c.Parity != 'N' {
		flags |= dcbParity
	}
	switch c.Flow {
	case "rtscts":
		flags |= dcbOutxCtsFlow | dcbRtsHandshake
	case "xonxoff":
		flags |= dcbOutX | dcbInX | dcbRtsEnable
		dcb.XonChar, dcb.XoffChar = 0x11, 0x13
		dcb.XonLim, dcb.XoffLim = 512, 512
	default:
		flags |= dcbRtsEnable
	}
	dcb.Flags = dcb.Flags&^dcbControlFields | flags
	if err := windows.SetCommState(h, &dcb); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return fmt.Errorf("장치가 이 설정(%d %d%c%d)을 지원하지 않습니다", c.Baud, c.DataBits, c.Parity, c.StopBits)
		}
		return err
	}
	// A read returns as soon as any byte arrives and otherwise waits (about
	// 49 days, see Read). A write that flow control holds back gives up after 5s.
	to := windows.CommTimeouts{
		ReadIntervalTimeout:        0xffffffff,
		ReadTotalTimeoutMultiplier: 0xffffffff,
		ReadTotalTimeoutConstant:   0xfffffffe,
		WriteTotalTimeoutConstant:  5000,
	}
	if err := windows.SetCommTimeouts(h, &to); err != nil {
		return err
	}
	return windows.PurgeComm(h, windows.PURGE_RXCLEAR|windows.PURGE_TXCLEAR)
}

func (s *serialSession) Read(p []byte) (int, error) {
	for {
		start := time.Now()
		n, err := s.f.Read(p)
		if s.closed.Load() {
			return n, io.EOF
		}
		// io.EOF after the long timeout is a read that got nothing: wait again.
		// Sooner, the device is gone (the driver no longer waits).
		if n == 0 && errors.Is(err, io.EOF) && time.Since(start) < 24*time.Hour {
			debugf("serial read: nothing after %v", time.Since(start))
			return 0, errSerialGone
		}
		if n > 0 || !errors.Is(err, io.EOF) {
			if err != nil && !errors.Is(err, io.EOF) {
				debugf("serial read: %v", err)
				err = errSerialGone
			}
			return n, err
		}
	}
}

func (s *serialSession) sendBreak() error {
	rc, err := s.f.SyscallConn()
	if err != nil {
		return err
	}
	var berr error
	err = rc.Control(func(fd uintptr) {
		h := windows.Handle(fd)
		if berr = windows.SetCommBreak(h); berr != nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
		berr = windows.ClearCommBreak(h)
	})
	if err != nil {
		return err
	}
	return berr
}

// listSerialPorts lists the COM ports Windows knows of, named by their
// device ("USB-SERIAL CH340") when Device Manager has a name for them.
func listSerialPorts() []SerialPort {
	var ports []SerialPort
	seen := map[string]bool{}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE); err == nil {
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			com, _, err := k.GetStringValue(n)
			if err != nil || seen[strings.ToUpper(com)] {
				continue
			}
			seen[strings.ToUpper(com)] = true
			ports = append(ports, SerialPort{Name: strings.ToUpper(com), Label: n[strings.LastIndex(n, `\`)+1:]})
		}
		k.Close()
	}
	friendly := comFriendlyNames()
	for i := range ports {
		if f := friendly[ports[i].Name]; f != "" {
			ports[i].Label = f
		}
	}
	sort.Slice(ports, func(i, j int) bool { return comNumber(ports[i].Name) < comNumber(ports[j].Name) })
	return ports
}

func comNumber(name string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(name, "COM"))
	return n
}

// comFriendlyNames maps "COM3" to its Device Manager name without the
// " (COM3)" at the end, for the ports present now.
func comFriendlyNames() map[string]string {
	names := map[string]string{}
	ports, err := windows.GUIDFromString("{4D36E978-E325-11CE-BFC1-08002BE10318}") // GUID_DEVCLASS_PORTS
	if err != nil {
		return names
	}
	set, err := windows.SetupDiGetClassDevsEx(&ports, "", 0, windows.DIGCF_PRESENT, 0, "")
	if err != nil {
		return names
	}
	defer set.Close()
	for i := 0; ; i++ {
		dev, err := set.EnumDeviceInfo(i)
		if err != nil {
			break
		}
		h, err := set.OpenDevRegKey(dev, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DEV, windows.KEY_READ)
		if err != nil {
			continue
		}
		k := registry.Key(h)
		com, _, err := k.GetStringValue("PortName")
		k.Close()
		if err != nil || !strings.HasPrefix(strings.ToUpper(com), "COM") {
			continue // LPT ports share the class
		}
		com = strings.ToUpper(com)
		if v, err := set.DeviceRegistryProperty(dev, windows.SPDRP_FRIENDLYNAME); err == nil {
			if s, ok := v.(string); ok {
				names[com] = strings.TrimSpace(strings.TrimSuffix(s, "("+com+")"))
			}
		}
	}
	return names
}
