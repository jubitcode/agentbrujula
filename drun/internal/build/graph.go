package build

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

type Step struct {
	Name         string
	Run          string
	DependsOn    []string
	InvalidateOn []string
	Cache        bool
	clean        bool
}

func NewStep(name, run string) *Step {
	return &Step{
		Name:  name,
		Run:   run,
		clean: false,
	}
}

func (s *Step) IsDirty() bool {
	return !s.clean
}

func (s *Step) MarkDirty() {
	s.clean = false
}

func (s *Step) MarkClean() {
	s.clean = true
}

func (s *Step) Hash() string {
	h := sha256.New()
	h.Write([]byte(s.Name))
	h.Write([]byte(s.Run))
	for _, dep := range s.DependsOn {
		h.Write([]byte(dep))
	}
	for _, inv := range s.InvalidateOn {
		h.Write([]byte(inv))
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

type Graph struct {
	steps map[string]*Step
}

func NewGraph() *Graph {
	return &Graph{
		steps: make(map[string]*Step),
	}
}

func (g *Graph) AddStep(step *Step) error {
	if _, exists := g.steps[step.Name]; exists {
		return fmt.Errorf("duplicate step: %s", step.Name)
	}
	g.steps[step.Name] = step
	return nil
}

func (g *Graph) GetStep(name string) *Step {
	return g.steps[name]
}

func (g *Graph) Len() int {
	return len(g.steps)
}

func (g *Graph) TopologicalSort() ([]*Step, error) {
	if len(g.steps) == 0 {
		return []*Step{}, nil
	}

	for _, step := range g.steps {
		for _, dep := range step.DependsOn {
			if _, exists := g.steps[dep]; !exists {
				return nil, fmt.Errorf("unknown dependency %q for step %q", dep, step.Name)
			}
		}
	}

	inDegree := make(map[string]int)
	for name := range g.steps {
		inDegree[name] = 0
	}

	dependents := make(map[string][]string)
	for name, step := range g.steps {
		for _, dep := range step.DependsOn {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	queue := make([]string, 0)
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	sort.Strings(queue)

	result := make([]*Step, 0, len(g.steps))

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		result = append(result, g.steps[name])

		deps := dependents[name]
		sort.Strings(deps)
		for _, dep := range deps {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(g.steps) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return result, nil
}

func (g *Graph) DetectCycle() error {
	color := make(map[string]int)

	var visit func(name string) error
	visit = func(name string) error {
		color[name] = 1

		step := g.steps[name]
		for _, dep := range step.DependsOn {
			if _, exists := g.steps[dep]; !exists {
				continue
			}

			switch color[dep] {
			case 1:
				return fmt.Errorf("cycle detected: %s -> %s", name, dep)
			case 0:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}

		color[name] = 2
		return nil
	}

	for name := range g.steps {
		if color[name] == 0 {
			if err := visit(name); err != nil {
				return err
			}
		}
	}

	return nil
}

func (g *Graph) InvalidateDownstream(name string) []string {
	dependents := make(map[string][]string)
	for stepName, step := range g.steps {
		for _, dep := range step.DependsOn {
			dependents[dep] = append(dependents[dep], stepName)
		}
	}

	visited := make(map[string]bool)
	queue := []string{name}
	result := make([]string, 0)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, dep := range dependents[current] {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				queue = append(queue, dep)

				if step := g.steps[dep]; step != nil {
					step.MarkDirty()
				}
			}
		}
	}

	return result
}
