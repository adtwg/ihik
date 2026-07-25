package auth

const (
	PermissionPlatformManage = "platform.manage"
	PermissionCustomerManage = "customer.manage"
	PermissionRouterManage   = "router.manage"
	PermissionPackageManage  = "package.manage"
	PermissionBillingManage  = "billing.manage"
	PermissionFinanceRead    = "finance.read"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleSuperAdmin: {
		PermissionPlatformManage: {},
	},
	RoleMitra: {
		PermissionCustomerManage: {},
		PermissionRouterManage:   {},
		PermissionPackageManage:  {},
		PermissionBillingManage:  {},
		PermissionFinanceRead:    {},
	},
}

func HasPermission(role, permission string) bool {
	permissions, exists := rolePermissions[role]
	if !exists {
		return false
	}
	_, allowed := permissions[permission]
	return allowed
}