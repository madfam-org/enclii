package services

import (
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// =============================================================================
// getRemainingServiceIDs
// =============================================================================

func TestGetRemainingServiceIDs(t *testing.T) {
	tests := []struct {
		name     string
		inDegree map[uuid.UUID]int
		wantLen  int
	}{
		{
			name: "multiple entries",
			inDegree: map[uuid.UUID]int{
				uuid.MustParse("00000000-0000-0000-0000-000000000001"): 1,
				uuid.MustParse("00000000-0000-0000-0000-000000000002"): 2,
				uuid.MustParse("00000000-0000-0000-0000-000000000003"): 0,
			},
			wantLen: 3,
		},
		{
			name:     "empty map",
			inDegree: map[uuid.UUID]int{},
			wantLen:  0,
		},
		{
			name: "single entry",
			inDegree: map[uuid.UUID]int{
				uuid.MustParse("00000000-0000-0000-0000-000000000001"): 5,
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRemainingServiceIDs(tt.inDegree)
			if len(got) != tt.wantLen {
				t.Errorf("getRemainingServiceIDs() returned %d IDs, want %d", len(got), tt.wantLen)
				return
			}

			// Verify all returned IDs are valid UUID strings that exist in the input map
			for _, idStr := range got {
				parsedID, err := uuid.Parse(idStr)
				if err != nil {
					t.Errorf("getRemainingServiceIDs() returned invalid UUID: %q", idStr)
					continue
				}
				if _, ok := tt.inDegree[parsedID]; !ok {
					t.Errorf("getRemainingServiceIDs() returned UUID %q not in input map", idStr)
				}
			}
		})
	}
}

// =============================================================================
// runTopologicalSort (standalone algorithm from deployment_groups_test.go)
// Extended test cases for deeper edge coverage
// =============================================================================

func TestTopologicalSortEmptyInput(t *testing.T) {
	layers, err := runTopologicalSort(nil, nil)
	if err != nil {
		t.Errorf("runTopologicalSort(nil, nil) unexpected error: %v", err)
	}
	if layers != nil {
		t.Errorf("runTopologicalSort(nil, nil) = %v, want nil", layers)
	}
}

func TestTopologicalSortEmptySlice(t *testing.T) {
	layers, err := runTopologicalSort([]uuid.UUID{}, nil)
	if err != nil {
		t.Errorf("runTopologicalSort([], nil) unexpected error: %v", err)
	}
	if layers != nil {
		t.Errorf("runTopologicalSort([], nil) = %v, want nil", layers)
	}
}

func TestTopologicalSortSingleServiceNoDeps(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	layers, err := runTopologicalSort([]uuid.UUID{id}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 1 || layers[0][0] != id {
		t.Errorf("expected single service %v in layer 0, got %v", id, layers[0])
	}
}

func TestTopologicalSortParallelServices(t *testing.T) {
	// Five services, zero dependencies = all in layer 0
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
	}

	layers, err := runTopologicalSort(ids, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 1 {
		t.Errorf("expected 1 layer for independent services, got %d", len(layers))
	}
	if len(layers[0]) != 5 {
		t.Errorf("expected 5 services in layer 0, got %d", len(layers[0]))
	}
}

func TestTopologicalSortLinearChain(t *testing.T) {
	// A -> B -> C -> D (A has no deps, D depends on C, C depends on B, B depends on A)
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	d := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	deps := []db.ServiceDependency{
		{ServiceID: b, DependsOnServiceID: a},
		{ServiceID: c, DependsOnServiceID: b},
		{ServiceID: d, DependsOnServiceID: c},
	}

	layers, err := runTopologicalSort([]uuid.UUID{a, b, c, d}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 4 {
		t.Fatalf("expected 4 layers for linear chain, got %d", len(layers))
	}

	// Build layer index
	layerOf := make(map[uuid.UUID]int)
	for i, layer := range layers {
		for _, id := range layer {
			layerOf[id] = i
		}
	}

	// Verify ordering
	if layerOf[a] >= layerOf[b] {
		t.Errorf("A (layer %d) should be before B (layer %d)", layerOf[a], layerOf[b])
	}
	if layerOf[b] >= layerOf[c] {
		t.Errorf("B (layer %d) should be before C (layer %d)", layerOf[b], layerOf[c])
	}
	if layerOf[c] >= layerOf[d] {
		t.Errorf("C (layer %d) should be before D (layer %d)", layerOf[c], layerOf[d])
	}
}

func TestTopologicalSortDiamondDependency(t *testing.T) {
	//       db (0)
	//      /    \
	//   cache  queue  (layer 1)
	//      \    /
	//       api (layer 2)
	dbID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	cacheID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	queueID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	apiID := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	deps := []db.ServiceDependency{
		{ServiceID: cacheID, DependsOnServiceID: dbID},
		{ServiceID: queueID, DependsOnServiceID: dbID},
		{ServiceID: apiID, DependsOnServiceID: cacheID},
		{ServiceID: apiID, DependsOnServiceID: queueID},
	}

	layers, err := runTopologicalSort([]uuid.UUID{dbID, cacheID, queueID, apiID}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers for diamond graph, got %d", len(layers))
	}

	layerOf := make(map[uuid.UUID]int)
	for i, layer := range layers {
		for _, id := range layer {
			layerOf[id] = i
		}
	}

	if layerOf[dbID] != 0 {
		t.Errorf("db should be in layer 0, got layer %d", layerOf[dbID])
	}
	if layerOf[cacheID] != 1 {
		t.Errorf("cache should be in layer 1, got layer %d", layerOf[cacheID])
	}
	if layerOf[queueID] != 1 {
		t.Errorf("queue should be in layer 1, got layer %d", layerOf[queueID])
	}
	if layerOf[apiID] != 2 {
		t.Errorf("api should be in layer 2, got layer %d", layerOf[apiID])
	}
}

func TestTopologicalSortCycleDetectionSimple(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	deps := []db.ServiceDependency{
		{ServiceID: a, DependsOnServiceID: b},
		{ServiceID: b, DependsOnServiceID: a},
	}

	_, err := runTopologicalSort([]uuid.UUID{a, b}, deps)
	if err == nil {
		t.Error("expected circular dependency error, got nil")
	}
}

func TestTopologicalSortCycleDetectionTriangle(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	deps := []db.ServiceDependency{
		{ServiceID: a, DependsOnServiceID: c},
		{ServiceID: b, DependsOnServiceID: a},
		{ServiceID: c, DependsOnServiceID: b},
	}

	_, err := runTopologicalSort([]uuid.UUID{a, b, c}, deps)
	if err == nil {
		t.Error("expected circular dependency error for triangle cycle, got nil")
	}
}

func TestTopologicalSortSelfDependency(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	deps := []db.ServiceDependency{
		{ServiceID: a, DependsOnServiceID: a},
	}

	_, err := runTopologicalSort([]uuid.UUID{a}, deps)
	if err == nil {
		t.Error("expected circular dependency error for self-dependency, got nil")
	}
}

func TestTopologicalSortOutOfScopeDependenciesIgnored(t *testing.T) {
	// Service A depends on service C, but C is not in the input set.
	// The dependency should be ignored.
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003") // not in scope

	deps := []db.ServiceDependency{
		{ServiceID: a, DependsOnServiceID: c},
		{ServiceID: b, DependsOnServiceID: c},
	}

	layers, err := runTopologicalSort([]uuid.UUID{a, b}, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(layers) != 1 {
		t.Errorf("expected 1 layer (out-of-scope deps ignored), got %d", len(layers))
	}
	if len(layers[0]) != 2 {
		t.Errorf("expected 2 services in layer 0, got %d", len(layers[0]))
	}
}

func TestTopologicalSortAllServicesPresent(t *testing.T) {
	ids := make([]uuid.UUID, 10)
	for i := range ids {
		ids[i] = uuid.New()
	}

	// Create a simple chain: 0->1->2->...->9
	deps := make([]db.ServiceDependency, 0, 9)
	for i := 1; i < 10; i++ {
		deps = append(deps, db.ServiceDependency{
			ServiceID:          ids[i],
			DependsOnServiceID: ids[i-1],
		})
	}

	layers, err := runTopologicalSort(ids, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count total services in output
	total := 0
	seen := make(map[uuid.UUID]bool)
	for _, layer := range layers {
		for _, id := range layer {
			if seen[id] {
				t.Errorf("service %v appears in multiple layers", id)
			}
			seen[id] = true
			total++
		}
	}
	if total != 10 {
		t.Errorf("expected all 10 services in output, got %d", total)
	}
}

// =============================================================================
// Dependency type parsing (mirrors logic in AddServiceDependency)
// =============================================================================

func TestDependencyTypeParsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    db.DependencyType
		wantErr bool
	}{
		{"runtime explicit", "runtime", db.DependencyTypeRuntime, false},
		{"runtime default (empty)", "", db.DependencyTypeRuntime, false},
		{"build", "build", db.DependencyTypeBuild, false},
		{"data", "data", db.DependencyTypeData, false},
		{"invalid type", "network", "", true},
		{"uppercase is invalid", "Runtime", "", true},
		{"whitespace is invalid", " runtime ", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var depType db.DependencyType
			var err error

			switch tt.input {
			case "build":
				depType = db.DependencyTypeBuild
			case "data":
				depType = db.DependencyTypeData
			case "runtime", "":
				depType = db.DependencyTypeRuntime
			default:
				err = &ValidationError{"Invalid type: must be runtime, build, or data"}
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
				return
			}

			if depType != tt.want {
				t.Errorf("dependency type for %q = %q, want %q", tt.input, depType, tt.want)
			}
		})
	}
}

// =============================================================================
// validateDependency (self-referential check)
// =============================================================================

func TestValidateDependencyExtended(t *testing.T) {
	tests := []struct {
		name        string
		serviceID   uuid.UUID
		dependsOnID uuid.UUID
		wantErr     bool
	}{
		{
			name:        "valid: different UUIDs",
			serviceID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			dependsOnID: uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			wantErr:     false,
		},
		{
			name:        "invalid: same UUID",
			serviceID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			dependsOnID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			wantErr:     true,
		},
		{
			name:        "valid: random UUIDs",
			serviceID:   uuid.New(),
			dependsOnID: uuid.New(),
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDependency(tt.serviceID, tt.dependsOnID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDependency() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// Layer ordering consistency — verify deterministic output for same input
// =============================================================================

func TestTopologicalSortDeterministicLayerContents(t *testing.T) {
	// Run the same sort multiple times and verify that the same services
	// always end up in the same layers (though order within a layer may vary).
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	d := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	deps := []db.ServiceDependency{
		{ServiceID: c, DependsOnServiceID: a},
		{ServiceID: c, DependsOnServiceID: b},
		{ServiceID: d, DependsOnServiceID: c},
	}
	ids := []uuid.UUID{a, b, c, d}

	// Run 10 times and collect layer assignments
	for i := 0; i < 10; i++ {
		layers, err := runTopologicalSort(ids, deps)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}

		layerOf := make(map[uuid.UUID]int)
		for li, layer := range layers {
			for _, id := range layer {
				layerOf[id] = li
			}
		}

		// a and b must be in layer 0, c in layer 1, d in layer 2
		if layerOf[a] != 0 || layerOf[b] != 0 {
			t.Errorf("iteration %d: a and b should be in layer 0, got a=%d b=%d", i, layerOf[a], layerOf[b])
		}
		if layerOf[c] != 1 {
			t.Errorf("iteration %d: c should be in layer 1, got %d", i, layerOf[c])
		}
		if layerOf[d] != 2 {
			t.Errorf("iteration %d: d should be in layer 2, got %d", i, layerOf[d])
		}
	}
}

// =============================================================================
// CircularDependencyError
// =============================================================================

func TestCircularDependencyErrorMessage(t *testing.T) {
	err := &CircularDependencyError{Message: "test cycle detected"}
	if err.Error() != "test cycle detected" {
		t.Errorf("CircularDependencyError.Error() = %q, want %q", err.Error(), "test cycle detected")
	}
}

// =============================================================================
// getRemainingServiceIDs returns sorted-stable output
// =============================================================================

func TestGetRemainingServiceIDsContainsAllKeys(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	inDegree := map[uuid.UUID]int{
		id1: 1,
		id2: 0,
		id3: 3,
	}

	got := getRemainingServiceIDs(inDegree)

	// Sort for deterministic comparison
	sort.Strings(got)
	want := []string{id1.String(), id2.String(), id3.String()}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("got %d IDs, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
