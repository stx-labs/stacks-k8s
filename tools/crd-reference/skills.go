package main

import "path/filepath"

// skillReference selects the chart CRDs rendered into one skill's references directory.
type skillReference struct {
	// Skill is the directory name under .agents/skills.
	Skill string
	// CRDs are repository-relative chart CRD directories.
	CRDs []string
	// Graph also renders graph.md from the skill's reference fields.
	Graph bool
}

// skillReferences lists every skill with generated CRD references.
var skillReferences = []skillReference{
	{Skill: "stacks-k8s-topology", CRDs: []string{"charts/stacks-network-operator/crds"}, Graph: true},
	{Skill: "stacks-k8s-faults", CRDs: []string{"charts/stacks-action-operator/crds"}},
	{Skill: "stacks-k8s-observability", CRDs: []string{"charts/stacks-observability-operator/crds"}},
}

// referenceDir is the skill's references directory under the repository root.
func (s skillReference) referenceDir(root string) string {
	return filepath.Join(root, ".agents", "skills", s.Skill, "references")
}

// crdDirs are the skill's CRD directories under the repository root.
func (s skillReference) crdDirs(root string) []string {
	dirs := make([]string, len(s.CRDs))
	for i, dir := range s.CRDs {
		dirs[i] = filepath.Join(root, filepath.FromSlash(dir))
	}
	return dirs
}
