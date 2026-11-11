package examples

import (
	sdk "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"testing"
)

func TestRelationAdditionPreservesPublishedEnumNames(t *testing.T) {
	values := []sdk.PersonalGuestRuntimeInputKind{sdk.Browser, sdk.Docker, sdk.Execute, sdk.FileRead, sdk.FileWrite}
	for i, want := range []string{"browser", "docker", "execute", "file_read", "file_write"} {
		if string(values[i]) != want {
			t.Fatalf("published enum %d changed", i)
		}
	}
	if sdk.JSON != sdk.ExportDeliveryDocumentParamsFormatJSON || sdk.Yaml != sdk.ExportDeliveryDocumentParamsFormatYaml {
		t.Fatal("published format aliases changed")
	}
}
