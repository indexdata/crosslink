package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
)

type DirectoryRole int

type AuthDataKey string //We don't want to use an actual string as the context key

const authDataKey AuthDataKey = "AUTH_DATA_KEY"

const (
	ConsortialAdminRole    DirectoryRole = 0
	InstitutionalAdminRole DirectoryRole = 1
	SystemUserRole         DirectoryRole = 2
	PublicUserRole         DirectoryRole = 3
)

const (
	FolioTenantHeader      = "X-Okapi-Tenant"
	FolioPermissionsHeader = "X-Okapi-Permissions"
)

type AuthData struct {
	institution string
	roleMap     map[DirectoryRole]bool
}

func (authData *AuthData) GetInstitution() string {
	return authData.institution
}

func (authData *AuthData) HasRole(role DirectoryRole) bool {
	if authData != nil {
		contains, ok := authData.roleMap[role]
		if !ok {
			return false
		}
		return contains
	}
	return false
}

func (authData *AuthData) HasRoleFromList(roles []DirectoryRole) bool {
	if authData == nil {
		return false
	}
	for _, role := range roles {
		contains, ok := authData.roleMap[role]
		if ok && contains {
			return true
		}
	}
	return false
}

func GetAuthData(cxt context.Context) *AuthData {
	authDataPtr, ok := cxt.Value(authDataKey).(*AuthData)
	if !ok {
		return nil
	}
	return authDataPtr
}

func contextWithAuthData(ctx context.Context, authDataPtr *AuthData) context.Context {
	return context.WithValue(ctx, authDataKey, authDataPtr)
}

var rolePermMap = map[string]DirectoryRole{
	"directory.consortium.all":  ConsortialAdminRole,
	"directory.institution.all": InstitutionalAdminRole,
	"directory.system.all":      SystemUserRole,
	"directory.public.all":      PublicUserRole,
}

func FolioTokenAwareMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var authData = AuthData{}
		var folioPermissions []string
		ctx := request.Context()
		//parse token out of request here, populate DirectoryContext
		folioTenant := request.Header.Get(FolioTenantHeader)
		if err := json.Unmarshal([]byte(request.Header.Get(FolioPermissionsHeader)), &folioPermissions); err != nil {
			folioPermissions = []string{}
		}

		authData.institution = folioTenant
		authData.roleMap = map[DirectoryRole]bool{}
		for key, val := range rolePermMap {
			if slices.Contains(folioPermissions, key) {
				authData.roleMap[val] = true
			}
		}
		ctx = contextWithAuthData(ctx, &authData)

		handler.ServeHTTP(writer, request.WithContext(ctx))
	})
}
