module github.com/indexdata/crosslink/illmock

go 1.26

require (
	github.com/indexdata/crosslink/directory v0.0.0
	github.com/indexdata/crosslink/httpclient v0.0.0
	github.com/indexdata/crosslink/iso18626 v0.0.0
	github.com/indexdata/crosslink/marcxml v0.0.0
	github.com/indexdata/crosslink/ncip v0.0.0
	github.com/indexdata/crosslink/sru v0.0.0
)

replace (
	github.com/indexdata/crosslink/directory => ../directory
	github.com/indexdata/crosslink/httpclient => ../httpclient
	github.com/indexdata/crosslink/iso18626 => ../iso18626
	github.com/indexdata/crosslink/marcxml => ../marcxml
	github.com/indexdata/crosslink/ncip => ../ncip
	github.com/indexdata/crosslink/sru => ../sru
)

require (
	github.com/google/uuid v1.6.0
	github.com/indexdata/cql-go v1.0.1-0.20260218123156-f3f18579fd7c
	github.com/indexdata/go-utils v0.0.0-20260218142542-28abe67711aa
	github.com/oapi-codegen/runtime v1.1.2
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
