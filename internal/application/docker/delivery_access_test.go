package docker

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestComposeAccessOnlyUsesPublishedTCPOnOwnedHost(t *testing.T) {
	content := "services:\n  web:\n    ports:\n      - '8080:80'\n      - '10.0.0.5:8081:81'\n      - '127.0.0.1:8082:82'\n      - '53:53/udp'\n      - '8000-8005:8000-8005'\n      - '80'\n      - { target: 443, published: 8443, host_ip: '0.0.0.0' }\n      - { target: 53, published: 8053, protocol: udp }\n"
	got := composeAccessCandidates(content, netip.MustParseAddr("10.0.0.5"))
	want := []domaindelivery.AccessCandidate{{URL: "http://10.0.0.5:8080/", DialAddress: "10.0.0.5", ProtocolUnknown: true}, {URL: "http://10.0.0.5:8081/", DialAddress: "10.0.0.5", ProtocolUnknown: true}, {URL: "http://10.0.0.5:8443/", DialAddress: "10.0.0.5", ProtocolUnknown: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates: %+v", got)
	}
	if port, bind := composePublishedPort("[2001:db8::1]:8080:80"); port != 8080 || bind != "2001:db8::1" {
		t.Fatal("IPv6 mapping lost")
	}
}

func TestDeliveryProjectAccessUsesFrozenConfigurationAndRejectsRetargeting(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host"] = domaindocker.Host{ID: "host", IPAddress: "10.0.0.5"}
	project := domaindocker.Project{ID: "project", HostID: "host", ComposeContent: "services:\n  api:\n    ports: ['8080:80']\n", EnvContent: "SECRET=hidden"}
	repo.projects["project"] = project
	key, _ := keyring.NewKey("test", "docker-access-test-encryption-key", time.Now().Add(-time.Hour), nil)
	keys, _ := keyring.New(key, nil)
	s := New(repo, dockerTestPermissions(), nil, WithCredentialEncryptionKeys(keys))
	frozen, err := s.FreezeDeliveryProject(context.Background(), dockerTestPrincipal(), "host", "project")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sohaapi.DockerDeliverySnapshot{ProjectID: "project", HostID: "host", ProjectDigest: frozen.ProjectDigest, RenderedDigest: frozen.RenderedDigest}
	project.ComposeContent = "services:\n  api:\n    ports: ['9000:80']\n"
	repo.projects["project"] = project
	entries, err := s.DeliveryProjectAccess(context.Background(), dockerTestPrincipal(), snapshot, frozen.Ciphertext)
	if err != nil || len(entries) != 1 || entries[0].URL != "http://10.0.0.5:8080/" {
		t.Fatalf("mutable config used: %+v %v", entries, err)
	}
	project.HostID = "other"
	repo.projects["project"] = project
	if _, err := s.DeliveryProjectAccess(context.Background(), dockerTestPrincipal(), snapshot, frozen.Ciphertext); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retarget accepted: %v", err)
	}
	project.HostID = "host"
	repo.projects["project"] = project
	snapshot.RenderedDigest = "other"
	if _, err := s.DeliveryProjectAccess(context.Background(), dockerTestPrincipal(), snapshot, frozen.Ciphertext); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong render accepted: %v", err)
	}
}
