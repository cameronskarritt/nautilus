package enums

type AuditType string

const (
	AuditTypeOrgAssume     AuditType = "org_assume"
	AuditTypeOrgUnassume   AuditType = "org_unassume"
	AuditTypeOrgFlagUpdate AuditType = "org_flag_update"
)

func (t AuditType) String() string {
	return string(t)
}
