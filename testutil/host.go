package testutil

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/testcontainers/testcontainers-go"
)

var (
	configureHostOnce sync.Once
	configureHostErr  error
)

// PrepareTestcontainers isolates this process's resources and selects IPv4 for
// a Docker endpoint resolved as localhost. Explicit overrides and remote or
// container gateway addresses are preserved. Call before Testcontainers access.
func PrepareTestcontainers(ctx context.Context) error {
	if err := IsolateTestcontainersSession(); err != nil {
		return fmt.Errorf("isolate Testcontainers session: %w", err)
	}
	configureHostOnce.Do(func() {
		if _, configured := os.LookupEnv("TESTCONTAINERS_HOST_OVERRIDE"); configured {
			return
		}
		provider, err := testcontainers.NewDockerProvider()
		if err != nil {
			configureHostErr = fmt.Errorf("create Docker provider: %w", err)
			return
		}
		defer func() {
			if err := provider.Close(); err != nil && configureHostErr == nil {
				configureHostErr = fmt.Errorf("close Docker provider: %w", err)
			}
		}()
		host, err := provider.DaemonHost(ctx)
		if err != nil {
			configureHostErr = fmt.Errorf("resolve Docker host: %w", err)
			return
		}
		configureHostErr = setLocalDockerHost(host)
	})
	return configureHostErr
}

func setLocalDockerHost(host string) error {
	if _, configured := os.LookupEnv("TESTCONTAINERS_HOST_OVERRIDE"); configured || host != "localhost" {
		return nil
	}
	// MappedPort selects the first (IPv4) binding. Docker can publish a different
	// IPv6 port, so resolving localhost to ::1 can reach another test's container.
	return os.Setenv("TESTCONTAINERS_HOST_OVERRIDE", "127.0.0.1")
}
