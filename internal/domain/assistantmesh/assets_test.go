package assistantmesh

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
)

func TestAssetInputValidateAndDigest(t *testing.T) {
	valid := AssetInput{Kind: KindSkill, Title: "Release", Body: "instructions", Applicability: "delivery", Files: map[string]string{"reference.md": "details", "data/values.yaml": "x: 1"}, Dependencies: []Dependency{{ID: "mcp-release", Version: "1.2.0"}}, Parameters: map[string]Parameter{"target": {Type: "string", Description: "target", Required: true}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid asset rejected: %v", err)
	}
	digest := valid.Digest()
	if len(digest) != 64 || digest != valid.Digest() {
		t.Fatalf("digest is not stable sha256: %q", digest)
	}
	if empty := (AssetInput{Kind: KindSkill, Title: "t", Body: "b"}); empty.Digest() != (AssetInput{Kind: KindSkill, Title: "t", Body: "b", Files: map[string]string{}, Dependencies: []Dependency{}, Parameters: map[string]Parameter{}}).Digest() {
		t.Fatal("nil and empty collections must normalize identically")
	}
	for name, mutate := range map[string]func(*AssetInput){
		"body":       func(in *AssetInput) { in.Body += "!" },
		"file":       func(in *AssetInput) { in.Files["reference.md"] += "!" },
		"dependency": func(in *AssetInput) { in.Dependencies[0].Version = "1.2.1" },
		"parameter": func(in *AssetInput) {
			in.Parameters["target"] = Parameter{Type: "integer", Description: "target", Required: true}
		},
		"applicability": func(in *AssetInput) { in.Applicability = "team" },
	} {
		t.Run("digest/"+name, func(t *testing.T) {
			changed := valid
			changed.Files = map[string]string{"reference.md": valid.Files["reference.md"], "data/values.yaml": valid.Files["data/values.yaml"]}
			changed.Dependencies = append([]Dependency(nil), valid.Dependencies...)
			changed.Parameters = map[string]Parameter{"target": valid.Parameters["target"]}
			mutate(&changed)
			if changed.Digest() == digest {
				t.Fatal("digest did not change")
			}
		})
	}
	payload, err := json.Marshal(valid)
	if err != nil || !strings.Contains(string(payload), `"applicability"`) || !strings.Contains(string(payload), `"dependencies"`) || !strings.Contains(string(payload), `"required"`) {
		t.Fatalf("JSON tags are not lowerCamel: %s (%v)", payload, err)
	}
}

func TestAssetInputRejectsInvalidContent(t *testing.T) {
	base := AssetInput{Kind: KindSkill, Title: "title", Body: "body"}
	for name, tc := range map[string]struct {
		input AssetInput
	}{
		"kind":                 {AssetInput{Kind: "tool", Title: "t", Body: "b"}},
		"empty title":          {AssetInput{Kind: KindSkill, Title: " ", Body: "b"}},
		"long title":           {AssetInput{Kind: KindSkill, Title: strings.Repeat("界", 257), Body: "b"}},
		"empty body":           {AssetInput{Kind: KindSkill, Title: "t", Body: "\n"}},
		"long body":            {AssetInput{Kind: KindSkill, Title: "t", Body: strings.Repeat("x", maxBodyBytes+1)}},
		"too many files":       {withFiles(base, manyFiles(maxAttachments+1))},
		"long file":            {withFiles(base, map[string]string{"a.md": strings.Repeat("x", maxAttachment+1)})},
		"absolute path":        {withFiles(base, map[string]string{"/a.md": "x"})},
		"traversal":            {withFiles(base, map[string]string{"../a.md": "x"})},
		"dot file":             {withFiles(base, map[string]string{".env.md": "x"})},
		"double slash":         {withFiles(base, map[string]string{"a//b.md": "x"})},
		"bad extension":        {withFiles(base, map[string]string{"run.sh": "x"})},
		"reserved name":        {withFiles(base, map[string]string{"nested/SKILL.MD": "x"})},
		"case duplicate":       {withFiles(base, map[string]string{"A.md": "x", "a.md": "y"})},
		"invalid dependency":   {withDependencies(base, []Dependency{{ID: "Upper", Version: "1"}})},
		"bad version":          {withDependencies(base, []Dependency{{ID: "ok", Version: "-1"}})},
		"duplicate dependency": {withDependencies(base, []Dependency{{ID: "ok", Version: "1"}, {ID: "ok", Version: "2"}})},
		"too many deps":        {withDependencies(base, repeatedDependencies(maxDependencies+1))},
		"invalid parameter":    {withParameters(base, map[string]Parameter{"x": {Type: "object", Description: "x"}})},
		"empty parameter":      {withParameters(base, map[string]Parameter{" ": {Type: "string", Description: "x"}})},
		"experience files":     {AssetInput{Kind: KindExperienceCard, Title: "t", Body: "b", Files: map[string]string{}}},
		"experience deps":      {AssetInput{Kind: KindExperienceCard, Title: "t", Body: "b", Dependencies: []Dependency{}}},
		"experience params":    {AssetInput{Kind: KindExperienceCard, Title: "t", Body: "b", Parameters: map[string]Parameter{}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tc.input.Validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v, want ErrInvalidInput", err)
			}
		})
	}
	if err := (AssetInput{Kind: KindExperienceCard, Title: "t", Body: "b", Applicability: "delivery"}).Validate(); err != nil {
		t.Fatalf("experience card with applicability rejected: %v", err)
	}
}

func TestAudienceRules(t *testing.T) {
	owner := identity.Principal{UserID: "owner", Teams: []string{"alpha", "beta"}, Projects: []string{"one"}}
	for name, scope := range map[string]knowledge.AccessScope{
		"private":      {Visibility: "private"},
		"teams":        {Visibility: "teams", Teams: []string{"alpha"}},
		"projects":     {Visibility: "projects", Projects: []string{"one"}},
		"organization": {Visibility: "organization"},
	} {
		t.Run("valid/"+name, func(t *testing.T) {
			if err := ValidateAudience(owner, scope); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, scope := range map[string]knowledge.AccessScope{
		"unknown visibility":     {Visibility: "public"},
		"users":                  {Visibility: "private", Users: []string{"x"}},
		"roles":                  {Visibility: "teams", Teams: []string{"alpha"}, Roles: []string{"admin"}},
		"private with team":      {Visibility: "private", Teams: []string{"alpha"}},
		"empty team grant":       {Visibility: "teams"},
		"team mixed":             {Visibility: "teams", Teams: []string{"alpha"}, Projects: []string{"one"}},
		"foreign team":           {Visibility: "teams", Teams: []string{"other"}},
		"duplicate team":         {Visibility: "teams", Teams: []string{"alpha", "alpha"}},
		"empty project grant":    {Visibility: "projects"},
		"foreign project":        {Visibility: "projects", Projects: []string{"two"}},
		"organization with team": {Visibility: "organization", Teams: []string{"alpha"}},
	} {
		t.Run("invalid/"+name, func(t *testing.T) {
			if err := ValidateAudience(owner, scope); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v, want ErrInvalidInput", err)
			}
		})
	}
	if err := ValidateAudience(identity.Principal{}, knowledge.AccessScope{Visibility: "organization"}); err == nil {
		t.Fatal("anonymous publisher accepted")
	}

	caller := identity.Principal{UserID: "caller", Teams: []string{"alpha"}, Projects: []string{"two"}}
	checks := []struct {
		name    string
		ownerID string
		scope   knowledge.AccessScope
		caller  identity.Principal
		want    bool
	}{
		{"private owner", "owner", knowledge.AccessScope{Visibility: "private"}, identity.Principal{UserID: "owner"}, true},
		{"private other", "owner", knowledge.AccessScope{Visibility: "private"}, caller, false},
		{"no identity", "owner", knowledge.AccessScope{Visibility: "organization"}, identity.Principal{}, false},
		{"team match", "owner", knowledge.AccessScope{Visibility: "teams", Teams: []string{"alpha"}}, caller, true},
		{"team miss", "owner", knowledge.AccessScope{Visibility: "teams", Teams: []string{"beta"}}, caller, false},
		{"project match", "owner", knowledge.AccessScope{Visibility: "projects", Projects: []string{"two"}}, caller, true},
		{"project miss", "owner", knowledge.AccessScope{Visibility: "projects", Projects: []string{"one"}}, caller, false},
		{"organization", "owner", knowledge.AccessScope{Visibility: "organization"}, caller, true},
		{"users ignored", "caller", knowledge.AccessScope{Visibility: "private", Users: []string{"caller"}}, caller, false},
		{"roles ignored", "owner", knowledge.AccessScope{Visibility: "organization", Roles: []string{"admin"}}, caller, false},
		{"unknown", "owner", knowledge.AccessScope{Visibility: "public"}, caller, false},
	}
	for _, tc := range checks {
		t.Run("allows/"+tc.name, func(t *testing.T) {
			if got := AudienceAllows(tc.ownerID, tc.scope, tc.caller); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func withFiles(base AssetInput, files map[string]string) AssetInput {
	base.Files = files
	return base
}

func withDependencies(base AssetInput, dependencies []Dependency) AssetInput {
	base.Dependencies = dependencies
	return base
}

func withParameters(base AssetInput, parameters map[string]Parameter) AssetInput {
	base.Parameters = parameters
	return base
}

func manyFiles(count int) map[string]string {
	files := make(map[string]string, count)
	for index := 0; index < count; index++ {
		files[strings.Repeat("a", index+1)+".md"] = "x"
	}
	return files
}

func repeatedDependencies(count int) []Dependency {
	dependencies := make([]Dependency, 0, count)
	for index := 0; index < count; index++ {
		dependencies = append(dependencies, Dependency{ID: "dep-" + strings.Repeat("a", index), Version: "1"})
	}
	return dependencies
}
