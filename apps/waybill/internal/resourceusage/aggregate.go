package resourceusage

import (
	"math"
	"sort"
)

const gib = float64(1 << 30)

// slotHours is the duration one sample stands for, in hours.
var slotHours = Step.Hours()

// WorkloadRow is one resource_daily row. A nil pointer is "not measured".
type WorkloadRow struct {
	Date         string `json:"date"`
	Cluster      string `json:"cluster"`
	Node         string `json:"node"`
	Namespace    string `json:"namespace"`
	WorkloadKind string `json:"workload_kind"`
	WorkloadName string `json:"workload_name"`
	Container    string `json:"container"`

	PodCount int `json:"pod_count"`

	CPUCoreHoursUsed      *float64 `json:"cpu_core_hours_used"`
	CPUCoreHoursRequested *float64 `json:"cpu_core_hours_requested"`
	CPULimitCoreHours     *float64 `json:"cpu_limit_core_hours"`
	CPUCoresP95           *float64 `json:"cpu_cores_p95"`
	CPUCoresMax           *float64 `json:"cpu_cores_max"`

	MemGiBHoursUsed       *float64 `json:"mem_gib_hours_used"`
	MemGiBHoursRequested  *float64 `json:"mem_gib_hours_requested"`
	MemLimitGiBHours      *float64 `json:"mem_limit_gib_hours"`
	MemWorkingSetP95Bytes *int64   `json:"mem_working_set_p95_bytes"`
	MemWorkingSetMaxBytes *int64   `json:"mem_working_set_max_bytes"`

	SampleCount int     `json:"sample_count"`
	Coverage    float64 `json:"coverage"`
	Source      string  `json:"source"`
}

// NodeRow is one node_daily row. A nil pointer is "not measured".
type NodeRow struct {
	Date    string `json:"date"`
	Cluster string `json:"cluster"`
	Node    string `json:"node"`

	PodCount int `json:"pod_count"`

	CPUAllocatableCoreHours *float64 `json:"cpu_allocatable_core_hours"`
	CPUCoreHoursRequested   *float64 `json:"cpu_core_hours_requested"`
	CPUCoreHoursUsed        *float64 `json:"cpu_core_hours_used"`
	CPUCoresP95             *float64 `json:"cpu_cores_p95"`
	CPUCoresMax             *float64 `json:"cpu_cores_max"`

	MemAllocatableGiBHours *float64 `json:"mem_allocatable_gib_hours"`
	MemGiBHoursRequested   *float64 `json:"mem_gib_hours_requested"`
	MemGiBHoursUsed        *float64 `json:"mem_gib_hours_used"`
	MemWorkingSetMaxBytes  *int64   `json:"mem_working_set_max_bytes"`

	SampleCount int     `json:"sample_count"`
	Coverage    float64 `json:"coverage"`
	Source      string  `json:"source"`
}

// Capture is one day's result: what the store writes in one transaction.
type Capture struct {
	Date, Cluster    string
	UsageCoverage    float64
	RequestsCoverage float64
	Workloads        []WorkloadRow
	Nodes            []NodeRow
	Source           string
}

// Empty reports whether Prometheus held nothing for the day.
func (c Capture) Empty() bool { return c.UsageCoverage == 0 && c.RequestsCoverage == 0 }

// series is one quantity sampled per slot; NaN marks a slot with no sample.
type series []float64

func newSeries(n int) series {
	s := make(series, n)
	for i := range s {
		s[i] = math.NaN()
	}
	return s
}

type instanceKey struct{ namespace, pod, container, node string }

// instance is one container of one pod on one node.
type instance struct {
	usage [2]series // CPUUsed, MemUsed: kept per slot for p95 and coverage
	sum   [numContainerMeasures]float64
	n     [numContainerMeasures]int
}

type nodeAcc struct {
	vals [numNodeMeasures]series
}

// accumulator folds a day's query results into rows.
type accumulator struct {
	day       Day
	slots     int
	instances map[instanceKey]*instance
	nodes     map[string]*nodeAcc
	usageAny  []bool
	reqAny    []bool
}

func newAccumulator(d Day) *accumulator {
	n := d.Slots()
	return &accumulator{
		day:       d,
		slots:     n,
		instances: map[instanceKey]*instance{},
		nodes:     map[string]*nodeAcc{},
		usageAny:  make([]bool, n),
		reqAny:    make([]bool, n),
	}
}

// addContainer folds the result of a per-container query. Each label set is
// unique per query (the PromQL groups by exactly these labels) and chunks of
// one day never overlap, so a (series, slot) pair arrives at most once.
func (a *accumulator) addContainer(m Measure, result []Series) {
	for _, s := range result {
		key := instanceKey{s.Labels["namespace"], s.Labels["pod"], s.Labels["container"], s.Labels["node"]}
		if key.namespace == "" || key.pod == "" || key.container == "" {
			continue
		}
		inst := a.instances[key]
		for _, p := range s.Points {
			i := a.day.SlotIndex(p.T)
			if i < 0 || p.V < 0 {
				continue
			}
			if inst == nil {
				inst = &instance{}
				a.instances[key] = inst
			}
			if m == CPUUsed || m == MemUsed {
				if inst.usage[m] == nil {
					inst.usage[m] = newSeries(a.slots)
				}
				inst.usage[m][i] = p.V
				a.usageAny[i] = true
			}
			if m == CPURequest || m == MemRequest {
				a.reqAny[i] = true
			}
			inst.sum[m] += p.V
			inst.n[m]++
		}
	}
}

// addNode folds the result of a per-node query.
func (a *accumulator) addNode(m Measure, result []Series) {
	for _, s := range result {
		node := s.Labels["node"]
		if node == "" {
			continue
		}
		for _, p := range s.Points {
			i := a.day.SlotIndex(p.T)
			if i < 0 || p.V < 0 {
				continue
			}
			acc := a.nodes[node]
			if acc == nil {
				acc = &nodeAcc{}
				a.nodes[node] = acc
			}
			if acc.vals[m] == nil {
				acc.vals[m] = newSeries(a.slots)
			}
			acc.vals[m][i] = p.V
			if m == NodeCPUUsed || m == NodeMemUsed {
				a.usageAny[i] = true
			}
		}
	}
}

type rowKey struct{ node, namespace, kind, name, container string }

// build turns the folded samples into rows, sorted for stable writes.
func (a *accumulator) build(cluster, source string, owners *Owners) Capture {
	c := Capture{
		Date:             a.day.Date,
		Cluster:          cluster,
		UsageCoverage:    fraction(countTrue(a.usageAny), a.slots),
		RequestsCoverage: fraction(countTrue(a.reqAny), a.slots),
		Source:           source,
	}

	groups := map[rowKey][]*instance{}
	nodePods := map[string]map[nsName]bool{}
	nodeReq := map[string]*[numContainerMeasures]float64{}
	nodeReqN := map[string]*[numContainerMeasures]int{}
	for key, inst := range a.instances {
		kind, name := owners.Resolve(key.namespace, key.pod)
		rk := rowKey{key.node, key.namespace, kind, name, key.container}
		groups[rk] = append(groups[rk], inst)

		if nodePods[key.node] == nil {
			nodePods[key.node] = map[nsName]bool{}
			nodeReq[key.node] = &[numContainerMeasures]float64{}
			nodeReqN[key.node] = &[numContainerMeasures]int{}
		}
		nodePods[key.node][nsName{key.namespace, key.pod}] = true
		for m := CPURequest; m < numContainerMeasures; m++ {
			nodeReq[key.node][m] += inst.sum[m]
			nodeReqN[key.node][m] += inst.n[m]
		}
	}

	for rk, insts := range groups {
		c.Workloads = append(c.Workloads, a.workloadRow(cluster, source, rk, insts))
	}
	sort.Slice(c.Workloads, func(i, j int) bool { return workloadLess(c.Workloads[i], c.Workloads[j]) })

	nodes := map[string]bool{}
	for n := range a.nodes {
		nodes[n] = true
	}
	for n := range nodePods {
		if n != "" {
			nodes[n] = true
		}
	}
	for node := range nodes {
		row := a.nodeRow(cluster, source, node)
		row.PodCount = len(nodePods[node])
		if req := nodeReq[node]; req != nil {
			row.CPUCoreHoursRequested = hoursIf(req[CPURequest], nodeReqN[node][CPURequest], 1)
			row.MemGiBHoursRequested = hoursIf(req[MemRequest], nodeReqN[node][MemRequest], gib)
		}
		c.Nodes = append(c.Nodes, row)
	}
	sort.Slice(c.Nodes, func(i, j int) bool { return c.Nodes[i].Node < c.Nodes[j].Node })
	return c
}

func (a *accumulator) workloadRow(cluster, source string, rk rowKey, insts []*instance) WorkloadRow {
	row := WorkloadRow{
		Date: a.day.Date, Cluster: cluster, Node: rk.node, Namespace: rk.namespace,
		WorkloadKind: rk.kind, WorkloadName: rk.name, Container: rk.container,
		PodCount: len(insts), Source: source,
	}
	present := make([]bool, a.slots)
	var sum [numContainerMeasures]float64
	var n [numContainerMeasures]int
	cpuP95, cpuMax, memP95, memMax := math.NaN(), math.NaN(), math.NaN(), math.NaN()
	for _, inst := range insts {
		for m := Measure(0); m < numContainerMeasures; m++ {
			sum[m] += inst.sum[m]
			n[m] += inst.n[m]
		}
		for i := 0; i < a.slots; i++ {
			if inst.has(CPUUsed, i) || inst.has(MemUsed, i) {
				present[i] = true
				row.SampleCount++
			}
		}
		if vals := inst.usage[CPUUsed].values(); len(vals) > 0 {
			cpuP95 = nanMax(cpuP95, quantile(0.95, vals))
			cpuMax = nanMax(cpuMax, vals[len(vals)-1])
		}
		if vals := inst.usage[MemUsed].values(); len(vals) > 0 {
			memP95 = nanMax(memP95, quantile(0.95, vals))
			memMax = nanMax(memMax, vals[len(vals)-1])
		}
	}
	row.Coverage = fraction(countTrue(present), a.slots)
	row.CPUCoreHoursUsed = hoursIf(sum[CPUUsed], n[CPUUsed], 1)
	row.CPUCoreHoursRequested = hoursIf(sum[CPURequest], n[CPURequest], 1)
	row.CPULimitCoreHours = hoursIf(sum[CPULimit], n[CPULimit], 1)
	row.MemGiBHoursUsed = hoursIf(sum[MemUsed], n[MemUsed], gib)
	row.MemGiBHoursRequested = hoursIf(sum[MemRequest], n[MemRequest], gib)
	row.MemLimitGiBHours = hoursIf(sum[MemLimit], n[MemLimit], gib)
	row.CPUCoresP95 = floatIf(cpuP95)
	row.CPUCoresMax = floatIf(cpuMax)
	row.MemWorkingSetP95Bytes = bytesIf(memP95)
	row.MemWorkingSetMaxBytes = bytesIf(memMax)
	return row
}

func (a *accumulator) nodeRow(cluster, source, node string) NodeRow {
	row := NodeRow{Date: a.day.Date, Cluster: cluster, Node: node, Source: source}
	acc := a.nodes[node]
	if acc == nil {
		return row
	}
	for i := 0; i < a.slots; i++ {
		if acc.vals[NodeCPUUsed].has(i) || acc.vals[NodeMemUsed].has(i) {
			row.SampleCount++
		}
	}
	row.Coverage = fraction(row.SampleCount, a.slots)
	row.CPUCoreHoursUsed = acc.vals[NodeCPUUsed].hours(1)
	row.CPUAllocatableCoreHours = acc.vals[NodeCPUAllocatable].hours(1)
	row.MemGiBHoursUsed = acc.vals[NodeMemUsed].hours(gib)
	row.MemAllocatableGiBHours = acc.vals[NodeMemAllocatable].hours(gib)
	if vals := acc.vals[NodeCPUUsed].values(); len(vals) > 0 {
		row.CPUCoresP95 = floatIf(quantile(0.95, vals))
		row.CPUCoresMax = floatIf(vals[len(vals)-1])
	}
	if vals := acc.vals[NodeMemUsed].values(); len(vals) > 0 {
		row.MemWorkingSetMaxBytes = bytesIf(vals[len(vals)-1])
	}
	return row
}

func (inst *instance) has(m Measure, i int) bool { return inst.usage[m].has(i) }

func (s series) has(i int) bool { return s != nil && !math.IsNaN(s[i]) }

// values returns the present samples, sorted ascending.
func (s series) values() []float64 {
	out := make([]float64, 0, len(s))
	for _, v := range s {
		if !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

// hours integrates a per-slot series: each sample stands for its 5 minutes.
func (s series) hours(unit float64) *float64 {
	var sum float64
	var n int
	for _, v := range s {
		if !math.IsNaN(v) {
			sum += v
			n++
		}
	}
	return hoursIf(sum, n, unit)
}

// hoursIf converts a sum of per-slot samples to unit-hours, or nil when
// there was no sample.
func hoursIf(sum float64, n int, unit float64) *float64 {
	if n == 0 {
		return nil
	}
	v := sum / unit * slotHours
	return &v
}

// quantile is Prometheus' φ-quantile over sorted values: linear
// interpolation between the closest ranks, as quantile_over_time does.
func quantile(q float64, sorted []float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	rank := q * float64(len(sorted)-1)
	lo := math.Floor(rank)
	hi := math.Ceil(rank)
	if lo == hi {
		return sorted[int(lo)]
	}
	w := rank - lo
	return sorted[int(lo)]*(1-w) + sorted[int(hi)]*w
}

func nanMax(a, b float64) float64 {
	if math.IsNaN(a) || b > a {
		return b
	}
	return a
}

func floatIf(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}

func bytesIf(v float64) *int64 {
	if math.IsNaN(v) {
		return nil
	}
	b := int64(math.Round(v))
	return &b
}

func countTrue(b []bool) int {
	n := 0
	for _, v := range b {
		if v {
			n++
		}
	}
	return n
}

func fraction(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func workloadLess(a, b WorkloadRow) bool {
	ka := [...]string{a.Namespace, a.WorkloadKind, a.WorkloadName, a.Container, a.Node}
	kb := [...]string{b.Namespace, b.WorkloadKind, b.WorkloadName, b.Container, b.Node}
	for i := range ka {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	return false
}
