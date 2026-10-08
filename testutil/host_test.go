package testutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetLocalDockerHost(t *testing.T) {
	for _, host := range []string{"localhost", "docker.example.org", "172.17.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "")
			require.NoError(t, os.Unsetenv("TESTCONTAINERS_HOST_OVERRIDE"))
			require.NoError(t, setLocalDockerHost(host))
			value, configured := os.LookupEnv("TESTCONTAINERS_HOST_OVERRIDE")
			if host == "localhost" {
				require.True(t, configured)
				require.Equal(t, "127.0.0.1", value)
			} else {
				require.False(t, configured)
			}
		})
	}
	for _, override := range []string{"docker.example.org", "localhost", ""} {
		t.Run("explicit="+override, func(t *testing.T) {
			t.Setenv("TESTCONTAINERS_HOST_OVERRIDE", override)
			require.NoError(t, setLocalDockerHost("localhost"))
			require.Equal(t, override, os.Getenv("TESTCONTAINERS_HOST_OVERRIDE"))
		})
	}
}
