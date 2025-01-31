package iso18626

//go:generate go run github.com/indexdata/xsd2goxsl ../xsd/ISO-18626-v1_2.xsd schema.go "qAttrImport=utils \"github.com/indexdata/go-utils/utils\"" qAttrType=utils.PrefixAttr dateTimeType=utils.XSDDateTime decimalType=utils.XSDDecimal json=yes
