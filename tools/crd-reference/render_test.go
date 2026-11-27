package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: stacks.stacks.org
  names: {kind: StacksSigner}
  versions:
  - name: v1alpha2
    served: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            x-kubernetes-validations:
            - {rule: "has(self.nodeRef)", message: "node required"}
            properties:
              nodeRef:
                description: NodeRef selects the node.
                properties:
                  name: {type: string, minLength: 1}
                required: [name]
                type: object
              mode:
                type: string
                enum: [A, B]
                default: A
              resources:
                type: object
                properties:
                  claims: {type: array, items: {type: object, properties: {name: {type: string}}}}
                  limits: {type: object, additionalProperties: {x-kubernetes-int-or-string: true}}
                  requests: {type: object, additionalProperties: {x-kubernetes-int-or-string: true}}
              items:
                type: array
                maxItems: 3
                x-kubernetes-list-type: map
                x-kubernetes-list-map-keys: [key]
                items:
                  type: object
                  required: [key]
                  x-kubernetes-validations:
                  - {rule: "self.key != ''", message: "key | required"}
                  properties:
                    key: {type: string}
              overrides:
                type: object
                properties:
                  stacksNode: {type: object, properties: {image: {type: string}}}
          status:
            type: object
`

func loadTestResource(t *testing.T) resource {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "signer.yaml"), []byte(testCRD), 0o600); err != nil {
		t.Fatal(err)
	}
	resources, err := loadResources([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Kind != "StacksSigner" || resources[0].Version != "v1alpha2" {
		t.Fatalf("resources: %+v", resources)
	}
	return resources[0]
}

func TestFlattenCollapsesReferencesCoreTypesAndBranches(t *testing.T) {
	res := loadTestResource(t)
	view := flatten(res.Kind, res.Spec)
	got := map[string]field{}
	for _, row := range view.Fields {
		got[row.Path] = row
	}
	for _, absent := range []string{"spec.nodeRef.name", "spec.resources.limits", "spec.overrides.stacksNode.image"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s should be collapsed", absent)
		}
	}
	if row := got["spec.nodeRef"]; !row.Reference {
		t.Errorf("nodeRef not a reference: %+v", row)
	}
	if row := got["spec.resources"]; row.Type != "core/v1 ResourceRequirements" {
		t.Errorf("resources type %q", row.Type)
	}
	if row := got["spec.overrides.stacksNode"]; !strings.HasSuffix(
		row.Description,
		"Same fields as StacksNode `spec`.",
	) {
		t.Errorf("branch description %q", row.Description)
	}
	if row := got["spec.items[].key"]; !row.Required {
		t.Errorf("items[].key should be required: %+v", row)
	}
	if row := got["spec.mode"]; strings.Join(row.Constraints, "; ") != "one of `A`, `B`; default `\"A\"`" {
		t.Errorf("mode constraints %q", row.Constraints)
	}
	if row := got["spec.items"]; strings.Join(row.Constraints, "; ") != "items ≤ 3; keyed by `key`" {
		t.Errorf("items constraints %q", row.Constraints)
	}
	if len(view.References) != 1 || view.References[0] != (reference{Kind: "StacksSigner", Path: "spec.nodeRef"}) {
		t.Errorf("references %+v", view.References)
	}
	wantRules := []rule{
		{Path: "spec", Message: "node required", Rule: "has(self.nodeRef)"},
		{Path: "spec.items[]", Message: "key | required", Rule: "self.key != ''"},
	}
	if len(view.Rules) != len(wantRules) || view.Rules[0] != wantRules[0] || view.Rules[1] != wantRules[1] {
		t.Errorf("rules %+v", view.Rules)
	}
}

func TestRenderFieldsEscapesCellsAndNamesTargets(t *testing.T) {
	res := loadTestResource(t)
	out := renderFields(res, flatten(res.Kind, res.Spec))
	for _, want := range []string{
		"| `spec.nodeRef` | ref → StacksNode | | NodeRef selects the node. | |",
		"| `spec.items[]` | key \\| required | `self.key != ''` |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCodeSpanWidensDelimiterAroundBackticks(t *testing.T) {
	if got := codeSpan("^[a-z]([-a-z]*)?$"); got != "`^[a-z]([-a-z]*)?$`" {
		t.Errorf("plain pattern %q", got)
	}
	if got := codeSpan("a`b"); got != "`` a`b ``" {
		t.Errorf("backtick text %q", got)
	}
}

func TestMergeRulesJoinsRepeatedMessages(t *testing.T) {
	merged := mergeRules([]rule{
		{Path: "spec", Message: "limit", Rule: "a"},
		{Path: "spec", Message: "other", Rule: "b"},
		{Path: "spec", Message: "limit", Rule: "c"},
	})
	if len(merged) != 2 || merged[0].Rule != "`a`; `c`" || merged[1].Rule != "`b`" {
		t.Fatalf("merged %+v", merged)
	}
}

func TestResolveReferencesReportsGapsInBothDirections(t *testing.T) {
	found := []reference{
		{Kind: "StacksSigner", Path: "spec.nodeRef"},
		{Kind: "StacksSigner", Path: "spec.unknownRef"},
		{Kind: "StacksGenesis", Path: "spec.ignoredRef"},
	}
	_, err := resolveReferences(found)
	if err == nil {
		t.Fatal("expected gaps")
	}
	for _, want := range []string{
		"undeclared reference target: StacksSigner spec.unknownRef",
		"declared reference no longer in schema: StacksNode spec.bitcoinNodeRef",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "StacksGenesis") {
		t.Errorf("generated kinds must be ignored: %v", err)
	}
}

func TestReplaceSectionKeepsHandWrittenText(t *testing.T) {
	page := []byte("# Title\n\nnotes\n\n" + beginMarker + "\nold\n" + endMarker + "\ntrailer\n")
	next, err := replaceSection(page, "new\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\nnotes\n\n" + beginMarker + "\n\nnew\n\n" + endMarker + "\ntrailer\n"
	if string(next) != want {
		t.Fatalf("got %q", next)
	}
	if _, err := replaceSection([]byte("no markers"), "x"); err == nil {
		t.Fatal("missing markers accepted")
	}
}

func TestRunFailsBeforeWritingOnStaleDeclarations(t *testing.T) {
	root := t.TempDir()
	crds := filepath.Join(root, "crds")
	skill := skillReference{Skill: "test", CRDs: []string{"crds"}, Graph: true}
	out := skill.referenceDir(root)
	for _, dir := range []string{crds, out} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(crds, "signer.yaml"), []byte(testCRD), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := "# page\n\n" + beginMarker + "\n" + endMarker + "\n"
	for _, page := range []string{"stackssigner.md", graphPage} {
		if err := os.WriteFile(filepath.Join(out, page), []byte(stub), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The fixture lacks most declared references, so resolution must fail before any write.
	if err := run(root, []skillReference{skill}, false); err == nil ||
		!strings.Contains(err.Error(), "declared reference no longer in schema") {
		t.Fatalf("expected stale declarations, got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "stackssigner.md")) //nolint:gosec // Test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != stub {
		t.Fatalf("page written despite failure: %q", data)
	}
}

func TestRepositoryPagesAreCurrent(t *testing.T) {
	if err := run(filepath.Join("..", ".."), skillReferences, true); err != nil {
		t.Fatal(err)
	}
}
