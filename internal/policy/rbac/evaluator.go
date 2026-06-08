package rbac

import domainaccess "github.com/opensoha/soha/internal/domain/access"

var RoleMatrix = map[string][]domainaccess.Action{
	"admin":     {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionCreate, domainaccess.ActionUpdate, domainaccess.ActionDelete, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger, domainaccess.ActionLogs, domainaccess.ActionExec},
	"ops":       {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionCreate, domainaccess.ActionUpdate, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger, domainaccess.ActionLogs},
	"developer": {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger, domainaccess.ActionLogs},
	"readonly":  {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs},
	"auditor":   {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch},
}
