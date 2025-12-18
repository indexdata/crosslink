module github.com/indexdata/crosslink/illmock

go 1.25

require (
	github.com/indexdata/crosslink/httpclient v0.0.0
	github.com/indexdata/crosslink/iso18626 v0.0.0
	github.com/indexdata/crosslink/marcxml v0.0.0
	github.com/indexdata/crosslink/sru v0.0.0
	github.com/indexdata/crosslink/ncip v0.0.0
)

replace (
	github.com/indexdata/crosslink/httpclient => ../httpclient
	github.com/indexdata/crosslink/iso18626 => ../iso18626
	github.com/indexdata/crosslink/marcxml => ../marcxml
	github.com/indexdata/crosslink/sru => ../sru
	github.com/indexdata/crosslink/ncip => ../ncip
)

require (
	github.com/google/uuid v1.6.0
	github.com/indexdata/cql-go v1.0.1-0.20250722084932-84f3837d6030
	github.com/indexdata/go-utils v0.0.0-20250210100229-d30dbd51df72
	github.com/oapi-codegen/runtime v1.1.2
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
