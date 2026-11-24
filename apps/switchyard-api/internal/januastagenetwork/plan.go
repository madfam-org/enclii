// Package januastagenetwork prepares only a fixed empty namespace and network
// configuration. Its receipt never establishes packet isolation or DB admission.
package januastagenetwork

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
)

const Profile = "janua-staging-network-preparation-v1"

var ErrRefused = errors.New("fixed Janua network preparation refused")
var ErrUnknown = errors.New("network preparation unconfirmed; inspect the same operation, do not retry creates")
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Peer struct {
	HostRoute string `json:"host_route"`
	Port      int32  `json:"port"`
}

// Inputs are supplied only by canonical native readers, never HTTP. Endpoint
// identities remain private journal data; responses expose only their digest.
type Inputs struct {
	Peers         []Peer               `json:"api_peers"`
	Operator      metav1.LabelSelector `json:"operator_selector"`
	ContextSHA256 string               `json:"context_sha256"`
}

type Plan struct {
	ID           uuid.UUID            `json:"operation_id"`
	ActorID      uuid.UUID            `json:"actor_id"`
	Authority    januastage.Authority `json:"authority"`
	Inputs       Inputs               `json:"native_inputs"`
	ObjectSHA256 []string             `json:"object_sha256"`
	SHA256       string               `json:"plan_sha256"`
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func BuildPlan(id, actor uuid.UUID, a januastage.Authority, in Inputs) (Plan, error) {
	if id == uuid.Nil || actor == uuid.Nil || januastage.ValidateAuthorityShape(a) != nil || !digest.MatchString(in.ContextSHA256) || len(in.Peers) == 0 || len(in.Peers) > 16 || len(in.Operator.MatchLabels) == 0 || len(in.Operator.MatchExpressions) != 0 {
		return Plan{}, ErrRefused
	}
	for key, value := range in.Operator.MatchLabels {
		if len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
			return Plan{}, ErrRefused
		}
	}
	seen := map[Peer]bool{}
	for _, p := range in.Peers {
		prefix, e := netip.ParsePrefix(p.HostRoute)
		if e != nil || prefix.Bits() != prefix.Addr().BitLen() || prefix.String() != p.HostRoute || prefix.Addr().IsUnspecified() || prefix.Addr().IsMulticast() || prefix.Addr().IsLoopback() || p.Port < 1 || p.Port > 65535 || seen[p] {
			return Plan{}, ErrRefused
		}
		seen[p] = true
	}
	in.Peers = append([]Peer(nil), in.Peers...)
	sort.Slice(in.Peers, func(i, j int) bool {
		if in.Peers[i].HostRoute == in.Peers[j].HostRoute {
			return in.Peers[i].Port < in.Peers[j].Port
		}
		return in.Peers[i].HostRoute < in.Peers[j].HostRoute
	})
	a.ObservedAt = ""
	p := Plan{ID: id, ActorID: actor, Authority: a, Inputs: in}
	for _, object := range p.Objects() {
		_, _, sha, e := Projection(object.Body)
		if e != nil {
			return Plan{}, ErrRefused
		}
		p.ObjectSHA256 = append(p.ObjectSHA256, sha)
	}
	p.SHA256 = Hash(p)
	return p, nil
}

func namespace(p Plan) string { return januastagedb.Namespace(p.Authority.ProjectID) }

func (p Plan) Validate() error {
	a := p.Authority
	a.ObservedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	w, e := BuildPlan(p.ID, p.ActorID, a, p.Inputs)
	if e != nil || p.Authority.ObservedAt != "" || !reflect.DeepEqual(w, p) {
		return ErrRefused
	}
	return nil
}

type Object struct {
	Key  string
	Body runtime.Object
}

func (p Plan) Objects() []Object {
	ns := januastagedb.Namespace(p.Authority.ProjectID)
	labels := map[string]string{k8s.LabelProjectNamespace: p.Authority.ProjectID.String(), "enclii.dev/managed-by": Profile, "enclii.dev/network-operation": p.ID.String()}
	meta := func(name, namespace string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}
	}
	namespace := &corev1.Namespace{ObjectMeta: meta(ns, "")}
	namespace.Labels = map[string]string{k8s.LabelProjectNamespace: p.Authority.ProjectID.String(), "enclii.dev/managed-by": Profile, "enclii.dev/network-operation": p.ID.String(), "enclii.dev/verify-signatures": "true", "pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"}
	port := func(n int32) []networkingv1.NetworkPolicyPort {
		tcp, x := corev1.ProtocolTCP, intstr.FromInt32(n)
		return []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &x}}
	}
	nsSelect := func(n string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": n, k8s.LabelProjectNamespace: p.Authority.ProjectID.String()}}
	}
	selector := func(k, v string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{k: v}}
	}
	api := selector("enclii.dev/service", januastage.Deployment)
	redis := selector("enclii.dev/service", "janua-staging-redis")
	db := selector("cnpg.io/cluster", januastagedb.Cluster)
	migrator := selector("enclii.dev/janua-migrator", januastagemigrate.Profile)
	peer := func(n string, s *metav1.LabelSelector) networkingv1.NetworkPolicyPeer {
		return networkingv1.NetworkPolicyPeer{NamespaceSelector: nsSelect(n), PodSelector: s}
	}
	policy := func(name, n string, s networkingv1.NetworkPolicySpec) Object {
		return Object{Key: "NetworkPolicy:" + n + "/" + name, Body: &networkingv1.NetworkPolicy{ObjectMeta: meta(name, n), Spec: s}}
	}
	egress := func(s *metav1.LabelSelector, n string, target *metav1.LabelSelector, number int32) networkingv1.NetworkPolicySpec {
		return networkingv1.NetworkPolicySpec{PodSelector: *s, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{peer(n, target)}, Ports: port(number)}}}
	}
	dns := intstr.FromInt32(53)
	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	baseline := policy("default-deny", ns, networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &dns}, {Protocol: &tcp, Port: &dns}}}}})
	// Keep the existing onboarding DNS baseline identity so existing fixed
	// readers can verify it; operation lineage is separately retained in052.
	baseline.Body.(*networkingv1.NetworkPolicy).Labels = map[string]string{k8s.LabelProjectNamespace: p.Authority.ProjectID.String(), "enclii.dev/managed-by": "onboarding-api"}
	dbEgress := []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{PodSelector: db}}, Ports: port(5432)}}
	for _, x := range p.Inputs.Peers {
		dbEgress = append(dbEgress, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: x.HostRoute}}}, Ports: port(x.Port)})
	}
	database := policy("janua-staging-db-isolation", ns, networkingv1.NetworkPolicySpec{PodSelector: *db, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Ingress: []networkingv1.NetworkPolicyIngressRule{
		{From: []networkingv1.NetworkPolicyPeer{{PodSelector: db}, {PodSelector: migrator}, peer(januastage.Namespace, api)}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "cnpg-system"}}, PodSelector: &p.Inputs.Operator}}, Ports: port(8000)},
	}, Egress: dbEgress})
	return []Object{{Key: "Namespace:/" + ns, Body: namespace}, baseline, database,
		policy("janua-staging-api-database", januastage.Namespace, egress(api, ns, db, 5432)),
		policy("janua-staging-migrator-database", ns, egress(migrator, ns, db, 5432)),
		policy("janua-staging-redis-isolation", januastage.Namespace, networkingv1.NetworkPolicySpec{PodSelector: *redis, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{peer(januastage.Namespace, api)}, Ports: port(6379)}}}),
		policy("janua-staging-api-redis", januastage.Namespace, egress(api, januastage.Namespace, redis, 6379))}
}

// Projection permits only Namespace API defaults, not added authority or rules.
func Projection(body runtime.Object) (string, string, string, error) {
	var m metav1.Object
	var spec any
	var kind string
	switch x := body.(type) {
	case *corev1.Namespace:
		x = x.DeepCopy()
		m = x
		kind = "Namespace"
		if label, exists := x.Labels["kubernetes.io/metadata.name"]; exists && label != x.Name {
			return "", "", "", ErrRefused
		}
		delete(x.Labels, "kubernetes.io/metadata.name")
		if len(x.Spec.Finalizers) > 1 || len(x.Spec.Finalizers) == 1 && x.Spec.Finalizers[0] != corev1.FinalizerKubernetes {
			return "", "", "", ErrRefused
		}
		spec = corev1.NamespaceSpec{}
	case *networkingv1.NetworkPolicy:
		m = x
		spec = x.Spec
		kind = "NetworkPolicy"
	default:
		return "", "", "", ErrRefused
	}
	if m.GetDeletionTimestamp() != nil || len(m.GetFinalizers()) != 0 {
		return "", "", "", ErrRefused
	}
	v := struct {
		Kind, Name, Namespace string
		Labels, Annotations   map[string]string
		Owners                []metav1.OwnerReference
		Spec                  any
	}{kind, m.GetName(), m.GetNamespace(), m.GetLabels(), m.GetAnnotations(), m.GetOwnerReferences(), spec}
	return string(m.GetUID()), m.GetResourceVersion(), Hash(v), nil
}
func Matches(want, actual runtime.Object) bool {
	_, _, a, e := Projection(want)
	_, _, b, f := Projection(actual)
	return e == nil && f == nil && reflect.DeepEqual(a, b)
}
