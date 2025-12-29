package common

import (
	"strings"
)

type Tenant struct {
	mapping string
}

func NewTenant(tenantSymbol string) Tenant {
	return Tenant{mapping: tenantSymbol}
}

func (t *Tenant) IsSpecified() bool {
	return t.mapping != ""
}

func (t *Tenant) GetSymbol(tenant string) string {
	return strings.ReplaceAll(t.mapping, "{tenant}", strings.ToUpper(tenant))
}
