package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// branchKinds maps typed participant inline/override/configuration branches to the kind whose spec they embed.
var branchKinds = map[string]string{
	"bitcoinBlockProduction":      "BitcoinBlockProduction",
	"bitcoinNode":                 "BitcoinNode",
	"stacksContractSet":           "StacksContractSet",
	"stacksFaucet":                "StacksFaucet",
	"stacksNode":                  "StacksNode",
	"stacksSigner":                "StacksSigner",
	"stacksStacker":               "StacksStacker",
	"stacksTransactionProduction": "StacksTransactionProduction",
}

// branchParents are the field names whose typed branches embed another kind's spec.
var branchParents = map[string]bool{"inline": true, "overrides": true, "configuration": true}

// field is one rendered spec row.
type field struct {
	Path        string
	Type        string
	Required    bool
	Description string
	Constraints []string
	Reference   bool
}

// rule is one CEL validation attached to a spec path.
type rule struct {
	Path    string
	Message string
	Rule    string
}

// reference is one name reference declared by a kind's spec.
type reference struct {
	Kind string
	Path string
}

// flattened is the rendered view of one kind's spec.
type flattened struct {
	Fields     []field
	Rules      []rule
	References []reference
}

// flatten walks a spec schema depth-first in property order.
func flatten(kind string, spec schema) flattened {
	var out flattened
	out.Rules = appendRules(out.Rules, "spec", spec.Validations)
	walk(&out, kind, "spec", "", spec)
	return out
}

func walk(out *flattened, kind, path, parentName string, node schema) {
	required := map[string]bool{}
	for _, name := range node.Required {
		required[name] = true
	}
	for _, name := range sortedKeys(node.Properties) {
		child := node.Properties[name]
		childPath := path + "." + name
		row := field{
			Path:        childPath,
			Type:        typeName(child),
			Required:    required[name],
			Description: oneLine(child.Description),
			Constraints: constraints(child),
		}
		if embedded, ok := branchKinds[name]; ok && branchParents[parentName] {
			row.Type = "object"
			row.Description = strings.TrimSpace(row.Description + " Same fields as " + embedded + " `spec`.")
			out.Fields = append(out.Fields, row)
			continue
		}
		out.Fields = append(out.Fields, row)
		elementPath, element := childPath, child
		if child.Type == "array" && child.Items != nil {
			elementPath, element = childPath+"[]", *child.Items
			out.Rules = appendRules(out.Rules, childPath, child.Validations)
		}
		out.Rules = appendRules(out.Rules, elementPath, element.Validations)
		if isReference(name, element) {
			out.Fields[len(out.Fields)-1].Reference = true
			out.References = append(out.References, reference{Kind: kind, Path: childPath})
			continue
		}
		if core := coreType(element); core != "" {
			out.Fields[len(out.Fields)-1].Type = strings.Replace(row.Type, "object", core, 1)
			continue
		}
		if len(element.Properties) > 0 {
			walk(out, kind, elementPath, name, element)
		}
	}
}

// coreType names embedded core/v1 structures whose upstream documentation is not repeated.
func coreType(element schema) string {
	switch {
	case hasExactly(element, "claims", "limits", "requests"):
		return "core/v1 ResourceRequirements"
	case hasExactly(element, "effect", "key", "operator", "tolerationSeconds", "value"):
		return "core/v1 Toleration"
	default:
		return ""
	}
}

func hasExactly(element schema, names ...string) bool {
	if len(element.Properties) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := element.Properties[name]; !ok {
			return false
		}
	}
	return true
}

// isReference reports a ref/*Ref/*Refs field whose element is a name or Secret key selector.
func isReference(name string, element schema) bool {
	if name != "ref" && !strings.HasSuffix(name, "Ref") && !strings.HasSuffix(name, "Refs") {
		return false
	}
	if _, ok := element.Properties["name"]; !ok {
		return false
	}
	for property := range element.Properties {
		if property != "name" && property != "key" && property != "uid" {
			return false
		}
	}
	return true
}

func appendRules(rules []rule, path string, validations []validationRule) []rule {
	for _, validation := range validations {
		rules = append(rules, rule{Path: path, Message: oneLine(validation.Message), Rule: oneLine(validation.Rule)})
	}
	return rules
}

func typeName(node schema) string {
	switch {
	case node.IntOrString:
		return "int-or-string"
	case node.Type == "array" && node.Items != nil:
		return "[]" + typeName(*node.Items)
	case node.Type == "object" && node.AdditionalProperties != nil:
		return "map[string]" + typeName(*node.AdditionalProperties)
	case node.Type == "object" && node.PreserveUnknown:
		return "object (free-form)"
	case node.Type == "" && node.PreserveUnknown:
		return "any"
	case node.Format != "":
		return node.Type + " (" + node.Format + ")"
	default:
		return node.Type
	}
}

func constraints(node schema) []string {
	var out []string
	if len(node.Enum) > 0 {
		values := make([]string, 0, len(node.Enum))
		for _, value := range node.Enum {
			values = append(values, codeSpan(strings.Trim(string(value), `"`)))
		}
		out = append(out, "one of "+strings.Join(values, ", "))
	}
	if len(node.Default) > 0 {
		out = append(out, "default "+codeSpan(string(node.Default)))
	}
	out = appendBounds(out, "", node.Minimum, node.Maximum)
	out = appendIntBounds(out, "length", node.MinLength, node.MaxLength)
	out = appendIntBounds(out, "items", node.MinItems, node.MaxItems)
	out = appendIntBounds(out, "properties", node.MinProperties, node.MaxProperties)
	if node.Pattern != "" {
		out = append(out, "pattern "+codeSpan(node.Pattern))
	}
	if node.ListType == "map" && len(node.ListMapKeys) > 0 {
		out = append(out, "keyed by `"+strings.Join(node.ListMapKeys, "`, `")+"`")
	} else if node.ListType == "set" {
		out = append(out, "unique items")
	}
	return out
}

func appendBounds(out []string, label string, minimum, maximum *float64) []string {
	format := func(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
	switch {
	case minimum != nil && maximum != nil:
		return append(out, strings.TrimSpace(label+" "+format(*minimum)+"–"+format(*maximum)))
	case minimum != nil:
		return append(out, strings.TrimSpace(label+" ≥ "+format(*minimum)))
	case maximum != nil:
		return append(out, strings.TrimSpace(label+" ≤ "+format(*maximum)))
	default:
		return out
	}
}

func appendIntBounds(out []string, label string, minimum, maximum *int64) []string {
	return appendBounds(out, label, toFloat(minimum), toFloat(maximum))
}

func toFloat(value *int64) *float64 {
	if value == nil {
		return nil
	}
	converted := float64(*value)
	return &converted
}

// renderFields renders one kind's generated reference section.
func renderFields(res resource, view flattened) string {
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"`%s/%s`, kind `%s`.\nGenerated from the chart CRD; do not edit.\n\n",
		res.Group,
		res.Version,
		res.Kind,
	)
	b.WriteString("| Field | Type | Required | Description | Constraints |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, row := range view.Fields {
		required := ""
		if row.Required {
			required = "yes"
		}
		typeText := cell(row.Type)
		if row.Reference {
			typeText = referenceType(res.Kind, row)
		}
		b.WriteString(tableRow(
			"`"+row.Path+"`", typeText, required, cell(row.Description), cell(strings.Join(row.Constraints, "; ")),
		))
	}
	if len(view.Rules) > 0 {
		b.WriteString("\nAdmission rules (CEL):\n\n")
		b.WriteString("| Path | Message | Rules |\n")
		b.WriteString("| --- | --- | --- |\n")
		for _, entry := range mergeRules(view.Rules) {
			b.WriteString(tableRow("`"+entry.Path+"`", cell(entry.Message), entry.Rule))
		}
	}
	return b.String()
}

// referenceType renders a reference row's type, naming its declared target when known.
func referenceType(kind string, row field) string {
	prefix := "ref"
	if strings.HasPrefix(row.Type, "[]") {
		prefix = "[]ref"
	}
	declared, ok := referenceTargets[kind+" "+row.Path]
	if !ok {
		return prefix
	}
	if declared.Scope == scopeSecret {
		return prefix + " → Secret key"
	}
	return prefix + " → " + cell(declared.Kind)
}

// mergeRules joins rules sharing a path and message into one row of code spans, keeping first-seen order.
func mergeRules(rules []rule) []rule {
	var merged []rule
	index := map[string]int{}
	for _, entry := range rules {
		code := codeSpan(cell(entry.Rule))
		key := entry.Path + "\x00" + entry.Message
		if position, ok := index[key]; ok {
			merged[position].Rule += "; " + code
			continue
		}
		index[key] = len(merged)
		merged = append(merged, rule{Path: entry.Path, Message: entry.Message, Rule: code})
	}
	return merged
}

// codeSpan renders inline code, widening the delimiter when the text contains a backtick.
func codeSpan(text string) string {
	if strings.Contains(text, "`") {
		return "`` " + text + " ``"
	}
	return "`" + text + "`"
}

// tableRow renders one Markdown table row, writing empty cells as a single space.
func tableRow(cells ...string) string {
	var b strings.Builder
	b.WriteString("|")
	for _, text := range cells {
		if text == "" {
			b.WriteString(" |")
			continue
		}
		b.WriteString(" " + text + " |")
	}
	b.WriteString("\n")
	return b.String()
}

// cell escapes text for a single Markdown table cell.
func cell(text string) string {
	return strings.ReplaceAll(text, "|", `\|`)
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
