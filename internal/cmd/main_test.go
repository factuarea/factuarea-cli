package cmd

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// Command tests must never prompt for, read or modify the user's OS keyring.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
