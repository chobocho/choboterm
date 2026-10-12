//go:build termux

package main

import "testing"

func TestTermuxDoesNotSupportSerialPorts(t *testing.T) {
	if serialSupported {
		t.Fatal("serialSupported = true, want false")
	}
	for _, host := range []string{"COM1", "/dev/ttyUSB0"} {
		if isSerial(host) {
			t.Errorf("isSerial(%q) = true, want false", host)
		}
	}
	if ports := listSerialPorts(); len(ports) != 0 {
		t.Errorf("listSerialPorts() = %v, want no ports", ports)
	}
	if _, err := openSerial(serialConfig{}); err == nil {
		t.Fatal("openSerial succeeded, want unsupported error")
	}
}
