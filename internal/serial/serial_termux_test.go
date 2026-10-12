//go:build termux

package serial

import "testing"

func TestTermuxDoesNotSupportSerialPorts(t *testing.T) {
	if Supported {
		t.Fatal("serialSupported = true, want false")
	}
	for _, host := range []string{"COM1", "/dev/ttyUSB0"} {
		if IsPort(host) {
			t.Errorf("isSerial(%q) = true, want false", host)
		}
	}
	if ports := ListPorts(); len(ports) != 0 {
		t.Errorf("listSerialPorts() = %v, want no ports", ports)
	}
	if _, err := Open(serialConfig{}); err == nil {
		t.Fatal("openSerial succeeded, want unsupported error")
	}
}
