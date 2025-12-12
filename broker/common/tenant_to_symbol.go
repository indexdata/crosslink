package common

import (
	"strings"
)

type TenantToSymbol struct {
	mapping string
}

func NewTenantToSymbol(tenantSymbol string) TenantToSymbol {
	return TenantToSymbol{mapping: tenantSymbol}
}

func (t *TenantToSymbol) TenantMode() bool {
	return t.mapping != ""
}

func (t *TenantToSymbol) GetSymbolFromTenant(tenant string) string {
	return strings.ReplaceAll(t.mapping, "{tenant}", strings.ToUpper(tenant))
}
