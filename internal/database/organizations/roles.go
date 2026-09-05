package organizations

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

func (r Role) String() string {
	return string(r)
}

func (r Role) IsValid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

// CanManageMembers returns true if the role can invite/remove members
func (r Role) CanManageMembers() bool {
	return r == RoleOwner || r == RoleAdmin
}

// CanManageOrg returns true if the role can update organization settings
func (r Role) CanManageOrg() bool {
	return r == RoleOwner || r == RoleAdmin
}

// CanDeleteOrg returns true if the role can delete the organization
func (r Role) CanDeleteOrg() bool {
	return r == RoleOwner
}
