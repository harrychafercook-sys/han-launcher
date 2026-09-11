//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func adminTokenPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "HAN Launcher")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "admin-session.dat"), nil
}

func protectAdminToken(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func storeAdminToken(token string) error {
	path, err := adminTokenPath()
	if err != nil {
		return err
	}
	protected, err := protectAdminToken([]byte(token), false)
	if err != nil {
		return err
	}
	return os.WriteFile(path, protected, 0600)
}

func loadAdminToken() (string, error) {
	path, err := adminTokenPath()
	if err != nil {
		return "", err
	}
	protected, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plain, err := protectAdminToken(protected, true)
	if err != nil {
		return "", fmt.Errorf("decrypt admin token: %w", err)
	}
	return string(plain), nil
}

func deleteAdminToken() error {
	path, err := adminTokenPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
