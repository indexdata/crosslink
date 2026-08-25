module github.com/indexdata/crosslink/illmock

go 1.27.0

require (
	github.com/indexdata/crosslink/httpclient v0.0.0
	github.com/indexdata/crosslink/iso18626 v0.0.0
	github.com/indexdata/crosslink/marcxml v0.0.0
	github.com/indexdata/crosslink/ncip v0.0.0
	github.com/indexdata/crosslink/sru v0.0.0
	github.com/indexdata/crosslink/testutil v0.0.0
)

replace (
	github.com/indexdata/crosslink/directory => ../directory
	github.com/indexdata/crosslink/httpclient => ../httpclient
	github.com/indexdata/crosslink/iso18626 => ../iso18626
	github.com/indexdata/crosslink/marcxml => ../marcxml
	github.com/indexdata/crosslink/ncip => ../ncip
	github.com/indexdata/crosslink/sru => ../sru
	github.com/indexdata/crosslink/testutil => ../testutil
)

require (
	github.com/getkin/kin-openapi v0.147.0
	github.com/google/uuid v1.6.0
	github.com/indexdata/cql-go v1.0.1
	github.com/indexdata/go-utils v1.0.0
	github.com/oapi-codegen/nethttp-middleware v1.2.0
	github.com/oapi-codegen/nullable v1.2.0
	github.com/oapi-codegen/runtime v1.7.0
	github.com/stretchr/testify v1.12.1
)

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.8.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dprotaso/go-yit v0.0.0-20250513224043-18a80f8f6df4 // indirect
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/lufia/plan9stats v0.0.0-20260802145828-341c2f0c90b5 // indirect
	github.com/magiconair/properties v1.18.11 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.3.3 // indirect
	github.com/moby/moby/api v1.55.0 // indirect
	github.com/moby/moby/client v0.5.1 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.7.0 // indirect
	github.com/moby/sys/user v0.4.1 // indirect
	github.com/moby/sys/userns v0.2.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/oapi-codegen/oapi-codegen/v2 v2.8.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/power-devops/perfstat v0.0.0-20260805114148-88456608a4f6 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/shirou/gopsutil/v4 v4.26.7 // indirect
	github.com/sirupsen/logrus v1.10.1 // indirect
	github.com/speakeasy-api/jsonpath v0.6.3 // indirect
	github.com/speakeasy-api/openapi v1.25.0 // indirect
	github.com/testcontainers/testcontainers-go v0.44.0 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
