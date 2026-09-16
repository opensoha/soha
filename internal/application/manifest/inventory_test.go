package manifest

import (
	"testing"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

func TestInventoryHealthRequiresEveryFrozenResource(t *testing.T) {
	root := domainmanifest.RenderedDocument{APIVersion: "argoproj.io/v1alpha1", Kind: "Application", Namespace: "demo", Name: "app", ContentDigest: "root-digest"}
	child := domainmanifest.RenderedDocument{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "web", ContentDigest: "child-digest"}
	for _, mode := range []string{"complete", "missing child", "missing root", "duplicate", "wrong digest", "no uid", "wrong namespace", "unhealthy"} {
		t.Run(mode, func(t *testing.T) {
			items := []domainmanifest.ResourceInventory{}
			for _, doc := range []domainmanifest.RenderedDocument{root, child} {
				items = append(items, domainmanifest.ResourceInventory{APIVersion: doc.APIVersion, Kind: doc.Kind, Namespace: doc.Namespace, Name: doc.Name, DesiredObjectDigest: doc.ContentDigest, UID: doc.Name + "-uid", Health: "healthy"})
			}
			switch mode {
			case "missing child":
				items = items[:1]
			case "missing root":
				items = items[1:]
			case "duplicate":
				items[1] = items[0]
			case "wrong digest":
				items[1].DesiredObjectDigest = "stale-digest"
			case "no uid":
				items[1].UID = ""
			case "wrong namespace":
				items[1].Namespace = "other"
			case "unhealthy":
				items[1].Health = "progressing"
			}
			if inventoryHealthy(items, []domainmanifest.RenderedDocument{root}, []domainmanifest.RenderedDocument{child}) != (mode == "complete") {
				t.Fatal("frozen inventory completeness was not enforced")
			}
		})
	}
}
