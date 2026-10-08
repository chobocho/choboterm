//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const puttySessionsKey = `Software\SimonTatham\PuTTY\Sessions`

// puttySessions reads the sessions PuTTY saved in the registry.
func puttySessions() ([]SavedSession, error) {
	root, err := registry.OpenKey(registry.CURRENT_USER, puttySessionsKey, registry.READ)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, errors.New("PuTTY에 저장된 세션이 없습니다")
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var list []SavedSession
	for _, name := range names {
		k, err := registry.OpenKey(root, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		str := func(n string) string { s, _, _ := k.GetStringValue(n); return s }
		port, _, _ := k.GetIntegerValue("PortNumber")
		v := puttyValues{
			HostName: str("HostName"), Protocol: str("Protocol"), UserName: str("UserName"),
			LineCodePage: str("LineCodePage"), PortNumber: int(port),
		}
		k.Close()
		if s, ok := fromPuTTY(name, v); ok {
			list = append(list, s)
		}
	}
	return list, nil
}
