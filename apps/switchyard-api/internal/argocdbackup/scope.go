package argocdbackup

// Fixed public install identities from upstream commit
// 30abed52c30dbb5cf8fdd32076c6bcc01799087c, plus the argocd Namespace.
// The non-HA and HA source SHA256 values are respectively
// 09cd91f8507f0d63ad98b1fe3a879a0cff79d585e296af99bb5a88aa7da571f8 and
// 97bfea5ff776292a88f03930d6845e106e0cc2d6e750c5ce51be72110e6dba11.
// No live layout, override or image is selected here.
const (
	nonHA uint8 = 1 << iota
	ha
)

var installScope = []struct {
	Key     Key
	layouts uint8
}{
	{Key{"apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "applications.argoproj.io"}, nonHA | ha},
	{Key{"apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "applicationsets.argoproj.io"}, nonHA | ha},
	{Key{"apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "appprojects.argoproj.io"}, nonHA | ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-dex-server"}, nonHA | ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-notifications-controller"}, nonHA | ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-redis"}, nonHA},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-redis-ha-haproxy"}, ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-repo-server"}, nonHA | ha},
	{Key{"apps/v1", "Deployment", "argocd", "argocd-server"}, nonHA | ha},
	{Key{"apps/v1", "StatefulSet", "argocd", "argocd-application-controller"}, nonHA | ha},
	{Key{"apps/v1", "StatefulSet", "argocd", "argocd-redis-ha-server"}, ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-application-controller-network-policy"}, nonHA | ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-applicationset-controller-network-policy"}, nonHA | ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-dex-server-network-policy"}, nonHA | ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-notifications-controller-network-policy"}, nonHA | ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-redis-ha-proxy-network-policy"}, ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-redis-ha-server-network-policy"}, ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-redis-network-policy"}, nonHA},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-repo-server-network-policy"}, nonHA | ha},
	{Key{"networking.k8s.io/v1", "NetworkPolicy", "argocd", "argocd-server-network-policy"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRole", "", "argocd-application-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRole", "", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRole", "", "argocd-server"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "argocd-application-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "argocd-server"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-application-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-dex-server"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-notifications-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-redis"}, nonHA},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-redis-ha"}, ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-redis-ha-haproxy"}, ha},
	{Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", "argocd-server"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-application-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-dex-server"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-notifications-controller"}, nonHA | ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-redis"}, nonHA},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-redis-ha"}, ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-redis-ha-haproxy"}, ha},
	{Key{"rbac.authorization.k8s.io/v1", "RoleBinding", "argocd", "argocd-server"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-cmd-params-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-gpg-keys-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-notifications-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-rbac-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-redis-ha-configmap"}, ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-redis-ha-health-configmap"}, ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-ssh-known-hosts-cm"}, nonHA | ha},
	{Key{"v1", "ConfigMap", "argocd", "argocd-tls-certs-cm"}, nonHA | ha},
	{Key{"v1", "Namespace", "", "argocd"}, nonHA | ha},
	{Key{"v1", "Secret", "argocd", "argocd-notifications-secret"}, nonHA | ha},
	{Key{"v1", "Secret", "argocd", "argocd-secret"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-dex-server"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-metrics"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-notifications-controller-metrics"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-redis"}, nonHA},
	{Key{"v1", "Service", "argocd", "argocd-redis-ha"}, ha},
	{Key{"v1", "Service", "argocd", "argocd-redis-ha-announce-0"}, ha},
	{Key{"v1", "Service", "argocd", "argocd-redis-ha-announce-1"}, ha},
	{Key{"v1", "Service", "argocd", "argocd-redis-ha-announce-2"}, ha},
	{Key{"v1", "Service", "argocd", "argocd-redis-ha-haproxy"}, ha},
	{Key{"v1", "Service", "argocd", "argocd-repo-server"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-server"}, nonHA | ha},
	{Key{"v1", "Service", "argocd", "argocd-server-metrics"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-application-controller"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-applicationset-controller"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-dex-server"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-notifications-controller"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-redis"}, nonHA},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-redis-ha"}, ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-redis-ha-haproxy"}, ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-repo-server"}, nonHA | ha},
	{Key{"v1", "ServiceAccount", "argocd", "argocd-server"}, nonHA | ha},
}

func fixedScope(topology string) ([]Key, error) {
	var selected uint8
	switch topology {
	case "NON_HA_LAYOUT_OBSERVED":
		selected = nonHA
	case "HA_LAYOUT_OBSERVED":
		selected = ha
	default:
		return nil, ErrHold
	}
	keys := make([]Key, 0, len(installScope))
	for _, item := range installScope {
		if item.layouts&selected != 0 {
			keys = append(keys, item.Key)
		}
	}
	return keys, nil
}
