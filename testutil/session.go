package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
)

var (
	isolateSessionOnce sync.Once
	isolateSessionErr  error
)

// IsolateTestcontainersSession gives the current test process its own
// Testcontainers session. Go runs test packages in separate processes, and a
// shared session allows one package's Ryuk cleanup to remove containers that
// are still in use by another package.
// Call this before any Testcontainers configuration or client access: its
// configuration is cached on the first read, including the session ID.
func IsolateTestcontainersSession() error {
	isolateSessionOnce.Do(func() {
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			isolateSessionErr = fmt.Errorf("generate Testcontainers session ID: %w", err)
			return
		}
		isolateSessionErr = os.Setenv("TESTCONTAINERS_SESSION_ID", "crosslink-"+hex.EncodeToString(token))
	})
	return isolateSessionErr
}
