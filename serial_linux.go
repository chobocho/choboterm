//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// serialRates are the speeds termios can set.
var serialRates = map[int]uint32{
	50: unix.B50, 75: unix.B75, 110: unix.B110, 134: unix.B134, 150: unix.B150,
	200: unix.B200, 300: unix.B300, 600: unix.B600, 1200: unix.B1200, 1800: unix.B1800,
	2400: unix.B2400, 4800: unix.B4800, 9600: unix.B9600, 19200: unix.B19200,
	38400: unix.B38400, 57600: unix.B57600, 115200: unix.B115200, 230400: unix.B230400,
	460800: unix.B460800, 500000: unix.B500000, 576000: unix.B576000, 921600: unix.B921600,
	1000000: unix.B1000000, 1152000: unix.B1152000, 1500000: unix.B1500000,
	2000000: unix.B2000000, 2500000: unix.B2500000, 3000000: unix.B3000000,
	3500000: unix.B3500000, 4000000: unix.B4000000,
}

// openSerial opens and sets up a serial port. The descriptor stays
// non-blocking, so os.File reads go through the poller and Close ends a read
// that is waiting for data.
func openSerial(c serialConfig) (*serialSession, error) {
	rate, ok := serialRates[c.Baud]
	if !ok {
		return nil, fmt.Errorf("지원하지 않는 속도(baud)입니다: %d", c.Baud)
	}
	fd, err := unix.Open(c.Name, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT):
			return nil, fmt.Errorf("%s 포트가 없습니다. 장치가 연결되어 있는지 확인하세요", c.Name)
		case errors.Is(err, unix.EACCES):
			return nil, fmt.Errorf("%s 포트를 열 권한이 없습니다. sudo usermod -aG dialout $USER 후 다시 로그인하세요", c.Name)
		case errors.Is(err, unix.EBUSY):
			return nil, fmt.Errorf("%s 포트를 다른 프로그램(또는 다른 탭)이 쓰고 있습니다", c.Name)
		}
		return nil, fmt.Errorf("%s 포트를 열 수 없습니다: %w", c.Name, err)
	}
	// Exclusive: a second open (another tab, another program) gets EBUSY.
	if err := unix.IoctlSetInt(fd, unix.TIOCEXCL, 0); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("%s 포트를 다른 프로그램(또는 다른 탭)이 쓰고 있습니다", c.Name)
	}
	if err := setupSerial(fd, c, rate); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("%s 포트를 설정할 수 없습니다: %w", c.Name, err)
	}
	return &serialSession{f: os.NewFile(uintptr(fd), c.Name)}, nil
}

// setupSerial puts the port in raw mode with the given line settings.
func setupSerial(fd int, c serialConfig, rate uint32) error {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR |
		unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY | unix.INPCK
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD
	t.Cflag |= unix.CREAD | unix.CLOCAL | rate
	t.Cflag |= map[int]uint32{5: unix.CS5, 6: unix.CS6, 7: unix.CS7, 8: unix.CS8}[c.DataBits]
	switch c.Parity {
	case 'E':
		t.Cflag |= unix.PARENB
	case 'O':
		t.Cflag |= unix.PARENB | unix.PARODD
	}
	if c.StopBits == 2 {
		t.Cflag |= unix.CSTOPB
	}
	switch c.Flow {
	case "rtscts":
		t.Cflag |= unix.CRTSCTS
	case "xonxoff":
		t.Iflag |= unix.IXON | unix.IXOFF
	}
	t.Ispeed, t.Ospeed = rate, rate
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		return err
	}
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH)
}

func (s *serialSession) Read(p []byte) (int, error) {
	n, err := s.f.Read(p)
	if s.closed.Load() {
		return n, io.EOF
	}
	// A device that went away reads as end of file or EIO.
	if err != nil && n == 0 {
		debugf("serial read: %v", err)
		err = errSerialGone
	}
	return n, err
}

func (s *serialSession) sendBreak() error {
	rc, err := s.f.SyscallConn()
	if err != nil {
		return err
	}
	var berr error
	err = rc.Control(func(fd uintptr) {
		berr = unix.IoctlSetInt(int(fd), unix.TCSBRK, 0) // 0.25 to 0.5 seconds
	})
	if err != nil {
		return err
	}
	return berr
}

// listSerialPorts lists USB serial adapters and on-board ports that have a
// device behind them, named by /dev/serial/by-id when it has a name for them.
func listSerialPorts() []SerialPort {
	byID := map[string]string{}
	if links, _ := filepath.Glob("/dev/serial/by-id/*"); links != nil {
		for _, l := range links {
			if dev, err := filepath.EvalSymlinks(l); err == nil {
				byID[dev] = strings.TrimPrefix(filepath.Base(l), "usb-")
			}
		}
	}
	var ports []SerialPort
	for _, pat := range []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/ttyAMA*", "/dev/rfcomm*"} {
		devs, _ := filepath.Glob(pat)
		for _, d := range devs {
			ports = append(ports, SerialPort{Name: d, Label: byID[d]})
		}
	}
	// ttyS0..31 always exist; only those with a real UART behind them are listed.
	devs, _ := filepath.Glob("/dev/ttyS*")
	for _, d := range devs {
		b, err := os.ReadFile("/sys/class/tty/" + filepath.Base(d) + "/type")
		if err == nil && strings.TrimSpace(string(b)) != "0" {
			ports = append(ports, SerialPort{Name: d, Label: byID[d]})
		}
	}
	sort.SliceStable(ports, func(i, j int) bool { return ports[i].Name < ports[j].Name })
	return ports
}
