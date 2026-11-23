package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"sigs.k8s.io/yaml"
)

// crd is the subset of a CustomResourceDefinition the reference renders.
type crd struct {
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Kind     string `json:"kind"`
			Singular string `json:"singular"`
		} `json:"names"`
		Versions []struct {
			Name   string `json:"name"`
			Served bool   `json:"served"`
			Schema struct {
				OpenAPIV3Schema schema `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// schema is the subset of a structural OpenAPI v3 schema the reference renders.
type schema struct {
	Type                 string            `json:"type"`
	Format               string            `json:"format"`
	Description          string            `json:"description"`
	Properties           map[string]schema `json:"properties"`
	Items                *schema           `json:"items"`
	AdditionalProperties *schema           `json:"additionalProperties"`
	Required             []string          `json:"required"`
	Enum                 []json.RawMessage `json:"enum"`
	Default              json.RawMessage   `json:"default"`
	Pattern              string            `json:"pattern"`
	Minimum              *float64          `json:"minimum"`
	Maximum              *float64          `json:"maximum"`
	MinLength            *int64            `json:"minLength"`
	MaxLength            *int64            `json:"maxLength"`
	MinItems             *int64            `json:"minItems"`
	MaxItems             *int64            `json:"maxItems"`
	MinProperties        *int64            `json:"minProperties"`
	MaxProperties        *int64            `json:"maxProperties"`
	IntOrString          bool              `json:"x-kubernetes-int-or-string"`
	PreserveUnknown      bool              `json:"x-kubernetes-preserve-unknown-fields"`
	ListType             string            `json:"x-kubernetes-list-type"`
	ListMapKeys          []string          `json:"x-kubernetes-list-map-keys"`
	Validations          []validationRule  `json:"x-kubernetes-validations"`
}

// validationRule is one CEL rule attached to a schema node.
type validationRule struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// resource is one served kind's spec schema.
type resource struct {
	Group   string
	Version string
	Kind    string
	Spec    schema
}

// loadResources reads every CRD in dirs and returns the served versions sorted by kind.
func loadResources(dirs []string) ([]resource, error) {
	var resources []resource
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			loaded, err := loadCRD(path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			resources = append(resources, loaded...)
		}
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Kind < resources[j].Kind })
	return resources, nil
}

func loadCRD(path string) ([]resource, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Paths come from the generator's declared skill CRD directories.
	if err != nil {
		return nil, err
	}
	var definition crd
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return nil, err
	}
	names := definition.Spec.Names
	if names.Kind == "" {
		return nil, errors.New("no kind")
	}
	var resources []resource
	for _, version := range definition.Spec.Versions {
		if !version.Served {
			continue
		}
		spec, ok := version.Schema.OpenAPIV3Schema.Properties["spec"]
		if !ok {
			return nil, fmt.Errorf("%s/%s has no spec schema", names.Kind, version.Name)
		}
		resources = append(resources, resource{
			Group:   definition.Spec.Group,
			Version: version.Name,
			Kind:    names.Kind,
			Spec:    spec,
		})
	}
	return resources, nil
}
