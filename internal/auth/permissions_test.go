package auth

import "testing"

func TestRolePermissions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		role       string
		permission string
		want       bool
	}{
		{name: "super admin manages platform", role: RoleSuperAdmin, permission: PermissionPlatformManage, want: true},
		{name: "super admin does not use tenant route", role: RoleSuperAdmin, permission: PermissionCustomerManage, want: false},
		{name: "mitra manages customer", role: RoleMitra, permission: PermissionCustomerManage, want: true},
		{name: "mitra cannot manage platform", role: RoleMitra, permission: PermissionPlatformManage, want: false},
		{name: "unknown role denied", role: "unknown", permission: PermissionFinanceRead, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := HasPermission(testCase.role, testCase.permission); got != testCase.want {
				t.Fatalf("HasPermission(%q, %q) = %v, want %v", testCase.role, testCase.permission, got, testCase.want)
			}
		})
	}
}