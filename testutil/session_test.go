package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestIsolateTestcontainersSession(t *testing.T) {
	if os.Getenv("CROSSLINK_SESSION_TEST_HELPER") == "1" {
		require.Equal(t, "shared-session", os.Getenv("TESTCONTAINERS_SESSION_ID"))
		require.NoError(t, IsolateTestcontainersSession())

		sessionID := os.Getenv("TESTCONTAINERS_SESSION_ID")
		require.Regexp(t, `^crosslink-[0-9a-f]{32}$`, sessionID)
		require.Equal(t, sessionID, testcontainers.SessionID())

		require.NoError(t, IsolateTestcontainersSession())
		require.Equal(t, sessionID, os.Getenv("TESTCONTAINERS_SESSION_ID"))
		require.Equal(t, sessionID, testcontainers.SessionID())
		require.NoError(t, os.WriteFile(os.Getenv("CROSSLINK_SESSION_TEST_OUTPUT"), []byte(sessionID), 0600))
		return
	}

	t.Setenv("CROSSLINK_SESSION_TEST_HELPER", "1")
	t.Setenv("TESTCONTAINERS_SESSION_ID", "shared-session")
	var sessionIDs []string
	for range 2 {
		outputPath := filepath.Join(t.TempDir(), "session-id")
		cmd := exec.Command(os.Args[0], "-test.run=^TestIsolateTestcontainersSession$")
		cmd.Env = append(os.Environ(), "CROSSLINK_SESSION_TEST_OUTPUT="+outputPath)
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "subprocess output:\n%s", output)
		sessionID, err := os.ReadFile(outputPath)
		require.NoError(t, err)
		sessionIDs = append(sessionIDs, string(sessionID))
	}
	require.NotEqual(t, sessionIDs[0], sessionIDs[1], "separate processes must use separate Testcontainers sessions")
}
