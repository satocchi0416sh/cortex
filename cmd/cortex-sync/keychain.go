package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const keychainService = "cortex-notion"

var errKeychainNotFound = errors.New("keychain entry not found")

// keychainSet stores token under the macOS login keychain as a generic-password
// entry (service=cortex-notion). -U replaces any existing entry idempotently.
func keychainSet(account, token string) error {
	if account == "" {
		account = defaultKeychainAccount()
	}
	cmd := exec.Command("security", "add-generic-password",
		"-U",
		"-s", keychainService,
		"-a", account,
		"-w", token,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("security add-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func keychainGet() (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", keychainService, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// security exits 44 ("The specified item could not be found") when missing.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return "", errKeychainNotFound
		}
		return "", fmt.Errorf("security find-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func keychainDelete() error {
	cmd := exec.Command("security", "delete-generic-password", "-s", keychainService)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return errKeychainNotFound
		}
		return fmt.Errorf("security delete-generic-password: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func defaultKeychainAccount() string {
	// Account name is purely informational for `security` lookups; service is the
	// real key. Use a stable string so re-running init updates the same entry.
	return "cortex-sync"
}
