package storagedriver

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestHandshakeWireVector(t *testing.T) {
	secret := make([]byte, LaunchSecretBytes)
	for i := range secret {
		secret[i] = byte(i + 32)
	}
	identity := Identity{PluginID: "example.storage", Version: "1.2.3", ProtocolMajor: 1, Capabilities: 127}
	request := HandshakeRequest{
		Expected: identity,
		Nonce:    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
		Proof:    "dnXDs5jLGkjrO3clXW_CTD3ZMdGHJw3qCW2ue7ipGMs",
	}
	response, serverToken, err := request.Accept(secret, identity)
	if err != nil {
		t.Fatal(err)
	}
	if response.Actual != identity || response.Proof != "ge1yRJbuJXn4wogVSxolsqDKn6fCA636vRSpxgkoNqo" {
		t.Fatal("unexpected identity or driver proof")
	}
	hostToken, err := response.Verify(secret, request)
	if err != nil || hostToken != serverToken || hostToken != "sn2-Bp14ngxhpzNwzxIqJRCsi8ttC57zB5Jhs1XLqRU" { //nolint:gosec // G101: fixed cross-language test vector, not a credential.
		t.Fatalf("unexpected handshake result: %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil || strings.Contains(string(encoded), serverToken) {
		t.Fatal("handshake response must not contain the bearer token")
	}
}

func TestHandshakeRejectsMismatches(t *testing.T) {
	secret := make([]byte, LaunchSecretBytes)
	identity := Identity{PluginID: "example.storage", Version: "1.2.3", ProtocolMajor: 1, Capabilities: V1Capabilities}
	request, err := NewHandshakeRequest(secret, identity)
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := request.Accept(secret, identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []Identity{
		{PluginID: "other.storage", Version: "1.2.3", ProtocolMajor: 1, Capabilities: V1Capabilities},
		{PluginID: "example.storage", Version: "1.2.4", ProtocolMajor: 1, Capabilities: V1Capabilities},
		{PluginID: "example.storage", Version: "1.2.3", ProtocolMajor: 2, Capabilities: V1Capabilities},
		{PluginID: "example.storage", Version: "1.2.3", ProtocolMajor: 1, Capabilities: CapabilityOpenObject},
	} {
		if _, token, err := request.Accept(secret, changed); token != "" || err != (RPCError{Code: CodeHandshakeMismatch}) {
			t.Errorf("accepted mismatched driver: %+v", changed)
		}
	}
	wrongSecret := make([]byte, LaunchSecretBytes)
	wrongSecret[0] = 1
	if _, _, err := request.Accept(wrongSecret, identity); err != (RPCError{Code: CodeUnauthenticated}) {
		t.Errorf("accepted wrong launch secret: %v", err)
	}
	newRequest, err := NewHandshakeRequest(secret, identity)
	if err != nil || newRequest.Nonce == request.Nonce {
		t.Fatalf("new request did not generate a fresh nonce: %v", err)
	}
	if _, err := response.Verify(secret, newRequest); err != (RPCError{Code: CodeUnauthenticated}) {
		t.Errorf("accepted proof for old nonce: %v", err)
	}
	response.Proof = request.Proof
	if _, err := response.Verify(secret, request); err != (RPCError{Code: CodeUnauthenticated}) {
		t.Errorf("accepted reflected host proof: %v", err)
	}
	if _, err := NewHandshakeRequest(secret[:31], identity); err != (RPCError{Code: CodeUnauthenticated}) {
		t.Errorf("accepted short launch secret: %v", err)
	}
}

func TestMetadataLimitsAndShape(t *testing.T) {
	exactLimit := `{"s":"` + strings.Repeat("x", MaxMetadataBytes-8) + `"}`
	encoded, err := EncodeMetadata(json.RawMessage(exactLimit))
	var target map[string]string
	if err != nil || DecodeMetadata(encoded, &target) != nil || target["s"] == "" {
		t.Fatalf("rejected metadata exactly at limit: %v", err)
	}
	tooLarge := `{"s":"` + strings.Repeat("x", MaxMetadataBytes-7) + `"}`
	if _, err := EncodeMetadata(json.RawMessage(tooLarge)); err != (RPCError{Code: CodeTooLarge}) {
		t.Errorf("accepted oversized metadata: %v", err)
	}
	oversizedHeader := base64.RawURLEncoding.EncodeToString([]byte(tooLarge))
	if err := DecodeMetadata(oversizedHeader, &target); err != (RPCError{Code: CodeTooLarge}) {
		t.Errorf("accepted oversized header: %v", err)
	}
	for _, raw := range []string{"null", "[]", "{} {}", `{"unexpected":true}`, "{\"key\":\"\xff\"}"} {
		header := base64.RawURLEncoding.EncodeToString([]byte(raw))
		var req OpenObjectRequest
		if err := DecodeMetadata(header, &req); err != (RPCError{Code: CodeInvalidRequest}) {
			t.Errorf("accepted invalid metadata %q: %v", raw, err)
		}
	}
	var req OpenObjectRequest
	if err := DecodeMetadata("e30=", &req); err != (RPCError{Code: CodeInvalidRequest}) {
		t.Errorf("accepted padded metadata: %v", err)
	}
}

func TestBindingScopeAndPageLimits(t *testing.T) {
	binding := Binding{ID: "binding1", Container: "objects", Prefix: "tenant/", Settings: json.RawMessage(`{"region":"local"}`)}
	put := PutObjectRequest{Binding: binding, Key: strings.Repeat("k", MaxKeyBytes-len(binding.Prefix)), ContentType: "text/plain"}
	if err := put.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{put.Key + "k", "../key", "a//key", "/key", `a\key`, "key\x00", ""} {
		bad := put
		bad.Key = key
		if bad.Validate() == nil {
			t.Errorf("accepted invalid relative key %q", key)
		}
	}
	badBinding := binding
	badBinding.Prefix = "tenant/../"
	if badBinding.Validate() == nil {
		t.Error("accepted noncanonical binding prefix")
	}
	badBinding = binding
	badBinding.Credentials = map[string]string{"test": strings.Repeat("x", (8<<10)+1)}
	if badBinding.Validate() == nil {
		t.Error("accepted oversized credential value")
	}
	keys := make([]string, MaxKeys+1)
	for i := range keys {
		keys[i] = "folder/duplicate"
	}
	if err := (DeleteKeysRequest{Binding: binding, Keys: keys[:MaxKeys]}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (DeleteKeysRequest{Binding: binding, Keys: keys}).Validate() == nil {
		t.Error("accepted oversized delete batch")
	}
	request := ListPrefixRequest{Binding: binding, Prefix: "folder/", Limit: MaxPageSize, Cursor: "first"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ListPrefixResult{Keys: []string{"folder/key"}, NextCursor: "second"}).Validate(request); err != nil {
		t.Fatal(err)
	}
	for _, page := range []ListPrefixResult{
		{Keys: nil},
		{Keys: []string{"other/key"}},
		{Keys: []string{"folder/key", "folder/key"}},
		{Keys: []string{}, NextCursor: "first"},
		{Keys: []string{}, NextCursor: strings.Repeat("x", MaxCursorBytes+1)},
	} {
		if page.Validate(request) == nil {
			t.Error("accepted invalid page")
		}
	}
	request.Limit++
	if request.Validate() == nil {
		t.Error("accepted excessive page size")
	}
}

func TestOpenConditionsAndRanges(t *testing.T) {
	request := OpenObjectRequest{
		Binding: Binding{ID: "binding1", Container: "objects"}, Key: "large/file",
		IfNoneMatch: `W/"same,with-comma", "other"`, Range: "bytes=4-8",
	}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (OpenObjectResult{ETag: `"same,with-comma"`, NotModified: true}).Validate(request); err != nil {
		t.Fatal(err)
	}
	if !MatchesIfNoneMatch("*", `W/"latest"`) || MatchesIfNoneMatch(`*, "other"`, `"other"`) {
		t.Fatal("unexpected wildcard matching")
	}
	for _, tc := range []struct {
		header string
		size   int64
		want   ContentRange
	}{
		{"bytes=4-8", 7, ContentRange{Start: 4, End: 6, Size: 7}},
		{"bytes=4-", 7, ContentRange{Start: 4, End: 6, Size: 7}},
		{"bytes=-2", 7, ContentRange{Start: 5, End: 6, Size: 7}},
		{"bytes=-99", 7, ContentRange{Start: 0, End: 6, Size: 7}},
		{"bytes=9223372036854775806-9223372036854775807", math.MaxInt64,
			ContentRange{Start: math.MaxInt64 - 1, End: math.MaxInt64 - 1, Size: math.MaxInt64}},
	} {
		got, err := ResolveByteRange(tc.header, tc.size)
		if err != nil || got == nil || *got != tc.want {
			t.Errorf("ResolveByteRange(%q, %d) = %+v, %v; want %+v", tc.header, tc.size, got, err, tc.want)
		}
	}
	for _, header := range []string{"bytes=4-3", "bytes=0-1,3-4", "bytes=-", "bytes=9223372036854775808-"} {
		if _, err := ResolveByteRange(header, 7); err != (RPCError{Code: CodeInvalidRequest}) {
			t.Errorf("accepted invalid range %q: %v", header, err)
		}
	}
	for _, header := range []string{"bytes=7-", "bytes=-0"} {
		if _, err := ResolveByteRange(header, 7); err != (RPCError{Code: CodeRangeNotSatisfiable}) {
			t.Errorf("accepted unsatisfiable range %q: %v", header, err)
		}
	}
	partial := OpenObjectResult{
		ETag: `"other-version"`, ContentType: MediaTypeBinary, ContentLength: 3,
		ContentRange: &ContentRange{Start: 4, End: 6, Size: 7}, Body: io.NopCloser(strings.NewReader("abc")),
	}
	defer func() { _ = partial.Body.Close() }()
	if err := partial.Validate(request); err != nil {
		t.Fatal(err)
	}
	partial.ContentLength++
	if partial.Validate(request) == nil {
		t.Error("accepted invalid length for partial body")
	}
	if CodeRangeNotSatisfiable.HTTPStatus() != http.StatusRequestedRangeNotSatisfiable {
		t.Error("range error does not map to HTTP 416")
	}
}
