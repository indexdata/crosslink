package testutil

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsolateTestcontainersSession(t *testing.T) {
	if os.Getenv("CROSSLINK_SESSION_TEST_HELPER") == "1" {
		require.Equal(t, "shared-session", os.Getenv("TESTCONTAINERS_SESSION_ID"))
		require.NoError(t, IsolateTestcontainersSession())

		sessionID := os.Getenv("TESTCONTAINERS_SESSION_ID")
		require.Regexp(t, `^crosslink-[0-9a-f]{32}$`, sessionID)

		require.NoError(t, IsolateTestcontainersSession())
		require.Equal(t, sessionID, os.Getenv("TESTCONTAINERS_SESSION_ID"))
		return
	}

	t.Setenv("CROSSLINK_SESSION_TEST_HELPER", "1")
	t.Setenv("TESTCONTAINERS_SESSION_ID", "shared-session")
	cmd := exec.Command(os.Args[0], "-test.run=^TestIsolateTestcontainersSession$")
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "subprocess output:\n%s", output)
}
