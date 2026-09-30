package testutil

import (
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
func IsolateTestcontainersSession() error {
	isolateSessionOnce.Do(func() {
		isolateSessionErr = os.Setenv("TESTCONTAINERS_SESSION_ID", fmt.Sprintf("crosslink-%d", os.Getpid()))
	})
	return isolateSessionErr
}
