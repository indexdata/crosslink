package testutil

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsolateTestcontainersSession(t *testing.T) {
	t.Setenv("TESTCONTAINERS_SESSION_ID", "shared-session")

	require.NoError(t, IsolateTestcontainersSession())
	require.Equal(t, fmt.Sprintf("crosslink-%d", os.Getpid()), os.Getenv("TESTCONTAINERS_SESSION_ID"))
}
