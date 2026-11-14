package catalog

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnpublishedFile is where "bundled but not installable" is written down.
const UnpublishedFile = "catalog/unpublished.yml"

//go:embed unpublished.yml
var unpublishedYAML []byte

// Unpublished is a bundled recipe the catalog deliberately does not publish.
// The reason is required: without it, "not in the catalog" and "somebody
// forgot" look identical.
type Unpublished struct {
	ID  string `yaml:"id"`
	Why string `yaml:"why"`
}

// LoadUnpublished parses the embedded list. Duplicates and empty reasons are
// refused for the same reason the recommendation list refuses them: this file
// is a decision record, not a scratch pad.
func LoadUnpublished() ([]Unpublished, error) {
	var doc struct {
		Recipes []Unpublished `yaml:"recipes"`
	}
	if err := yaml.Unmarshal(unpublishedYAML, &doc); err != nil {
		return nil, fmt.Errorf("catalog: %s: %w", UnpublishedFile, err)
	}
	seen := map[string]bool{}
	for i, e := range doc.Recipes {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("catalog: %s: entry %d has no id", UnpublishedFile, i+1)
		}
		if strings.TrimSpace(e.Why) == "" {
			return nil, fmt.Errorf("catalog: %s: %s has no why", UnpublishedFile, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("catalog: %s: %s is listed twice", UnpublishedFile, e.ID)
		}
		seen[e.ID] = true
	}
	return doc.Recipes, nil
}
