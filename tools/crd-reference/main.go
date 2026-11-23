// Command crd-reference renders per-kind CRD field references, and optionally a reference graph, for agent skills.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "<!-- BEGIN GENERATED: crd-reference -->"
	endMarker   = "<!-- END GENERATED: crd-reference -->"
	graphPage   = "graph.md"
)

func main() {
	var root string
	var check bool
	flag.StringVar(&root, "root", "", "repository root")
	flag.BoolVar(&check, "check", false, "report stale pages without writing")
	flag.Parse()
	if root == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: crd-reference -root DIR [-check]")
		os.Exit(2)
	}
	if err := run(root, skillReferences, check); err != nil {
		fmt.Fprintln(os.Stderr, "crd-reference:", err)
		os.Exit(1)
	}
}

// skillPages is the rendered generated sections of one skill, keyed by page name.
type skillPages struct {
	dir      string
	sections map[string]string
}

// run renders every skill's generated sections and either writes or checks them. Reference targets are
// checked against the union of all skills, so nothing is written unless every declaration is current.
func run(root string, skills []skillReference, check bool) error {
	var found []reference
	rendered := make([]skillPages, 0, len(skills))
	skillKinds := make([]map[string]bool, 0, len(skills))
	for _, skill := range skills {
		resources, err := loadResources(skill.crdDirs(root))
		if err != nil {
			return err
		}
		pages := skillPages{dir: skill.referenceDir(root), sections: map[string]string{}}
		kinds := map[string]bool{}
		for _, res := range resources {
			view := flatten(res.Kind, res.Spec)
			pages.sections[pageName(res.Kind)] = renderFields(res, view)
			found = append(found, view.References...)
			kinds[res.Kind] = true
		}
		rendered = append(rendered, pages)
		skillKinds = append(skillKinds, kinds)
	}
	resolved, err := resolveReferences(found)
	if err != nil {
		return err
	}
	for i, skill := range skills {
		if !skill.Graph {
			continue
		}
		var own []resolvedReference
		for _, ref := range resolved {
			if skillKinds[i][ref.From.Kind] {
				own = append(own, ref)
			}
		}
		rendered[i].sections[graphPage] = renderGraph(own)
	}

	var stale []string
	for _, pages := range rendered {
		for _, page := range sortedKeys(pages.sections) {
			path := filepath.Join(pages.dir, page)
			changed, err := updatePage(path, pages.sections[page], check)
			if err != nil {
				return err
			}
			if changed {
				stale = append(stale, path)
			}
		}
	}
	if check && len(stale) > 0 {
		return fmt.Errorf("stale generated reference (run make generate): %s", strings.Join(stale, ", "))
	}
	return nil
}

// updatePage replaces the marked section of one page, reporting whether its content changed.
func updatePage(path, section string, check bool) (bool, error) {
	current, err := os.ReadFile(path) //nolint:gosec // Paths are generator-owned page names under a skill directory.
	if err != nil {
		return false, fmt.Errorf("%s: %w (create it with %s and %s markers)", path, err, beginMarker, endMarker)
	}
	next, err := replaceSection(current, section)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(current, next) {
		return false, nil
	}
	if check {
		return true, nil
	}
	return true, os.WriteFile(path, next, 0o644) //nolint:gosec // Tracked documentation is world-readable.
}

func replaceSection(page []byte, section string) ([]byte, error) {
	begin := bytes.Index(page, []byte(beginMarker))
	end := bytes.Index(page, []byte(endMarker))
	if begin < 0 || end < begin {
		return nil, errors.New("missing or misordered generated-section markers")
	}
	var out bytes.Buffer
	out.Write(page[:begin+len(beginMarker)])
	out.WriteString("\n\n")
	out.WriteString(section)
	out.WriteString("\n")
	out.Write(page[end:])
	return out.Bytes(), nil
}
