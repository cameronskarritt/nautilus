package enums

type AuditType string

const (
	AuditTypeDocumentUpload  AuditType = "document_upload"
	AuditTypeDocumentContent AuditType = "document_content"
	AuditTypeOrgAssume       AuditType = "org_assume"
	AuditTypeOrgUnassume     AuditType = "org_unassume"
	AuditTypeOrgFlagUpdate   AuditType = "org_flag_update"
)

func (t AuditType) String() string {
	return string(t)
}
