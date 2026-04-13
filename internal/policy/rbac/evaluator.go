package rbac

import domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"

var RoleMatrix = map[string][]domainaccess.Action{
	"admin":     {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionUpdate, domainaccess.ActionDelete, domainaccess.ActionRestart, domainaccess.ActionScale, domainaccess.ActionLogs, domainaccess.ActionExec},
	"ops":       {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionUpdate, domainaccess.ActionRestart, domainaccess.ActionScale, domainaccess.ActionLogs},
	"developer": {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionRestart, domainaccess.ActionScale, domainaccess.ActionLogs},
	"readonly":  {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs},
	"auditor":   {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch},
}
