//go:build !windows

package main

import "errors"

func storeAdminToken(string) error    { return errors.New("secure token storage is unavailable") }
func loadAdminToken() (string, error) { return "", nil }
func deleteAdminToken() error         { return nil }
