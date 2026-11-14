export const SERVICE_ACCOUNT_DEFAULT_TEMPLATE = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-service-account
`

export const ROLE_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: example-role
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
`

export const ROLE_BINDING_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example-rolebinding
subjects:
  - kind: ServiceAccount
    name: example-service-account
    namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-role
`

export const CLUSTER_ROLE_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: example-cluster-role
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list"]
`

export const CLUSTER_ROLE_BINDING_DEFAULT_TEMPLATE = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: example-cluster-rolebinding
subjects:
  - kind: User
    name: example-user
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: example-cluster-role
`
