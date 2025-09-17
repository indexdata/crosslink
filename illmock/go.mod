module github.com/indexdata/crosslink/illmock

go 1.25

require (
	github.com/indexdata/crosslink/httpclient v0.0.0
	github.com/indexdata/crosslink/iso18626 v0.0.0
	github.com/indexdata/crosslink/marcxml v0.0.0
	github.com/indexdata/crosslink/sru v0.0.0
)

replace (
	github.com/indexdata/crosslink/httpclient => ../httpclient
	github.com/indexdata/crosslink/iso18626 => ../iso18626
	github.com/indexdata/crosslink/marcxml => ../marcxml
	github.com/indexdata/crosslink/sru => ../sru
)

require (
	github.com/google/uuid v1.6.0
	github.com/indexdata/cql-go v1.0.1-0.20250722084932-84f3837d6030
	github.com/indexdata/go-utils v0.0.0-20250210100229-d30dbd51df72
	github.com/magiconair/properties v1.8.10
	github.com/oapi-codegen/runtime v1.1.2
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
