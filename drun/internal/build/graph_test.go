package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphAddStep(t *testing.T) {
	g := NewGraph()
	err := g.AddStep(&Step{Name: "build", Run: "go build ."})
	require.NoError(t, err)

	assert.Equal(t, 1, g.Len())
}

func TestGraphAddDuplicateStep(t *testing.T) {
	g := NewGraph()
	err := g.AddStep(&Step{Name: "build", Run: "go build ."})
	require.NoError(t, err)

	err = g.AddStep(&Step{Name: "build", Run: "go build -v ."})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestGraphTopologicalSortNoDeps(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a"}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b"}))
	require.NoError(t, g.AddStep(&Step{Name: "c", Run: "cmd c"}))

	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, sorted, 3)
}

func TestGraphTopologicalSortWithDeps(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "test", Run: "go test", DependsOn: []string{"build"}}))
	require.NoError(t, g.AddStep(&Step{Name: "build", Run: "go build", DependsOn: []string{"codegen"}}))
	require.NoError(t, g.AddStep(&Step{Name: "codegen", Run: "go generate"}))

	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, sorted, 3)

	// codegen should come before build
	codegenIdx := indexOf(sorted, "codegen")
	buildIdx := indexOf(sorted, "build")
	testIdx := indexOf(sorted, "test")

	assert.Less(t, codegenIdx, buildIdx)
	assert.Less(t, buildIdx, testIdx)
}

func TestGraphTopologicalSortDiamond(t *testing.T) {
	// Diamond pattern: A -> B, A -> C, B -> D, C -> D
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a"}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "c", Run: "cmd c", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "d", Run: "cmd d", DependsOn: []string{"b", "c"}}))

	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	require.Len(t, sorted, 4)

	aIdx := indexOf(sorted, "a")
	bIdx := indexOf(sorted, "b")
	cIdx := indexOf(sorted, "c")
	dIdx := indexOf(sorted, "d")

	// a must come before b and c
	assert.Less(t, aIdx, bIdx)
	assert.Less(t, aIdx, cIdx)
	// b and c must come before d
	assert.Less(t, bIdx, dIdx)
	assert.Less(t, cIdx, dIdx)
}

func TestGraphCycleDetection(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a", DependsOn: []string{"c"}}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "c", Run: "cmd c", DependsOn: []string{"b"}}))

	err := g.DetectCycle()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestGraphCycleDetectionNoCycle(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a"}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "c", Run: "cmd c", DependsOn: []string{"b"}}))

	err := g.DetectCycle()
	assert.NoError(t, err)
}

func TestGraphTopologicalSortWithCycle(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a", DependsOn: []string{"b"}}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b", DependsOn: []string{"a"}}))

	_, err := g.TopologicalSort()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestGraphInvalidateDownstream(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "deps", Run: "go mod download"}))
	require.NoError(t, g.AddStep(&Step{Name: "build", Run: "go build", DependsOn: []string{"deps"}}))
	require.NoError(t, g.AddStep(&Step{Name: "test", Run: "go test", DependsOn: []string{"build"}}))

	// Invalidate deps
	invalidated := g.InvalidateDownstream("deps")

	// Should invalidate build and test
	assert.Contains(t, invalidated, "build")
	assert.Contains(t, invalidated, "test")
	assert.Len(t, invalidated, 2)
}

func TestGraphInvalidateDownstreamSingle(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a"}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b"}))

	// No downstream from a since b doesn't depend on a
	invalidated := g.InvalidateDownstream("a")
	assert.Empty(t, invalidated)
}

func TestGraphInvalidateDownstreamMultipleBranches(t *testing.T) {
	// a -> b -> d
	// a -> c -> d
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a"}))
	require.NoError(t, g.AddStep(&Step{Name: "b", Run: "cmd b", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "c", Run: "cmd c", DependsOn: []string{"a"}}))
	require.NoError(t, g.AddStep(&Step{Name: "d", Run: "cmd d", DependsOn: []string{"b", "c"}}))

	invalidated := g.InvalidateDownstream("a")
	assert.Contains(t, invalidated, "b")
	assert.Contains(t, invalidated, "c")
	assert.Contains(t, invalidated, "d")
	assert.Len(t, invalidated, 3)
}

func TestGraphGetStep(t *testing.T) {
	g := NewGraph()
	step := &Step{Name: "build", Run: "go build"}
	require.NoError(t, g.AddStep(step))

	got := g.GetStep("build")
	assert.NotNil(t, got)
	assert.Equal(t, "go build", got.Run)
}

func TestGraphGetStepNotFound(t *testing.T) {
	g := NewGraph()

	got := g.GetStep("nonexistent")
	assert.Nil(t, got)
}

func TestGraphUnknownDependency(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "build", Run: "go build", DependsOn: []string{"nonexistent"}}))

	_, err := g.TopologicalSort()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown dependency")
}

func TestGraphEmpty(t *testing.T) {
	g := NewGraph()

	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Empty(t, sorted)
}

func TestGraphSelfReference(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.AddStep(&Step{Name: "a", Run: "cmd a", DependsOn: []string{"a"}}))

	err := g.DetectCycle()
	assert.Error(t, err)
}

func TestStepIsDirty(t *testing.T) {
	step := &Step{Name: "build", Run: "go build"}
	assert.True(t, step.IsDirty())

	step.MarkClean()
	assert.False(t, step.IsDirty())

	step.MarkDirty()
	assert.True(t, step.IsDirty())
}

func TestStepHash(t *testing.T) {
	step := &Step{
		Name:      "build",
		Run:       "go build .",
		DependsOn: []string{"deps"},
	}

	hash1 := step.Hash()
	assert.NotEmpty(t, hash1)

	step2 := &Step{
		Name:      "build",
		Run:       "go build -v .",
		DependsOn: []string{"deps"},
	}

	hash2 := step2.Hash()
	assert.NotEqual(t, hash1, hash2)
}

func TestStepHashConsistent(t *testing.T) {
	step := &Step{
		Name:      "build",
		Run:       "go build .",
		DependsOn: []string{"deps"},
	}

	hash1 := step.Hash()
	hash2 := step.Hash()
	assert.Equal(t, hash1, hash2)
}

// Helper function to find index of step name in sorted list
func indexOf(steps []*Step, name string) int {
	for i, s := range steps {
		if s.Name == name {
			return i
		}
	}
	return -1
}
