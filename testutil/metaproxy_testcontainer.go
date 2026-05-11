package testutil

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const metaproxyBackendTestXML = `<?xml version="1.0"?>
<filter xmlns="http://indexdata.com/metaproxy" type="backend_test"/>
`

type MetaproxyContainer struct {
	mappedPort string
	container  testcontainers.Container
}

func MetaproxyContainerStart(ctx context.Context) (*MetaproxyContainer, error) {
	c := &MetaproxyContainer{}

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/indexdata/metaproxy:sha-c8a458f",
		ExposedPorts: []string{"9000/tcp"},
		WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(5 * time.Second),
		Files: []testcontainers.ContainerFile{
			{
				Reader:            bytes.NewReader([]byte(metaproxyBackendTestXML)),
				ContainerFilePath: "/etc/metaproxy/filters-enabled/backend_test.xml",
				FileMode:          0444, // Read-only
			},
		},
	}

	var err error
	c.container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}
	port, err := c.container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		_ = c.container.Terminate(ctx)
		return nil, err
	}
	c.mappedPort = port.Port()

	for i := 0; i < 10; i++ { // retry a few times to allow metaproxy to start up and load filters
		res, err := http.Get("http://localhost:" + c.mappedPort)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		// Metaproxy most likely returns 400 on this one, but we just want to verify that it responds at all
		err = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close response body: %w", err)
		}
		return c, nil
	}
	err = c.container.Terminate(ctx)
	if err != nil {
		return nil, fmt.Errorf("metaproxy did not start in time, and failed to terminate container: %w", err)
	}
	return nil, fmt.Errorf("metaproxy did not start in time")
}

func (c *MetaproxyContainer) MappedPort() string {
	return c.mappedPort
}

func (c *MetaproxyContainer) Terminate(ctx context.Context) error {
	if c.container != nil {
		return c.container.Terminate(ctx)
	}
	return nil
}
