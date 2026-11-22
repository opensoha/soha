package storagedriver

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	credentialKey  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	errInvalidKey  = errors.New("invalid binding-relative object key")
	errInvalidETag = errors.New("invalid entity tag")
)

// EncodeMetadata packs a JSON object for HeaderMetadata using unpadded base64url.
func EncodeMetadata(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || !jsonObject(raw) {
		return "", RPCError{Code: CodeInvalidRequest}
	}
	if len(raw) > MaxMetadataBytes {
		return "", RPCError{Code: CodeTooLarge}
	}
	return tokenEncoding.EncodeToString(raw), nil
}

// DecodeMetadata bounds both the encoded and decoded sizes before parsing the object.
func DecodeMetadata(value string, target any) error {
	if len(value) > tokenEncoding.EncodedLen(MaxMetadataBytes) {
		return RPCError{Code: CodeTooLarge}
	}
	raw, err := tokenEncoding.DecodeString(value)
	if err != nil || tokenEncoding.EncodeToString(raw) != value || !jsonObject(raw) {
		return RPCError{Code: CodeInvalidRequest}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return RPCError{Code: CodeInvalidRequest}
	}
	return nil
}

func (b Binding) Validate() error {
	if !boundedText(b.ID, 255, false) || !boundedText(b.Container, 255, false) {
		return errors.New("invalid binding ID or container")
	}
	if b.Prefix != "" && (!strings.HasSuffix(b.Prefix, "/") ||
		len(b.Prefix) >= MaxKeyBytes || !validKey(strings.TrimSuffix(b.Prefix, "/"), false)) {
		return errors.New("invalid binding prefix")
	}
	if len(b.Settings) != 0 && (len(b.Settings) > MaxMetadataBytes || !jsonObject(b.Settings)) {
		return errors.New("invalid binding settings")
	}
	if len(b.Credentials) > 32 {
		return errors.New("too many binding credentials")
	}
	credentialBytes := 0
	for key, value := range b.Credentials {
		if !credentialKey.MatchString(key) || len(value) > 8<<10 || !utf8.ValidString(value) {
			return errors.New("invalid binding credential")
		}
		credentialBytes += len(key) + len(value)
	}
	if credentialBytes+len(b.Settings) > MaxMetadataBytes {
		return RPCError{Code: CodeTooLarge}
	}
	_, err := EncodeMetadata(b)
	return err
}

func (r PutObjectRequest) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if !r.Binding.validRelativeKey(r.Key, false) {
		return errInvalidKey
	}
	if !validMediaType(r.ContentType) {
		return errors.New("invalid object content type")
	}
	return nil
}

func (r PutObjectResult) Validate() error {
	if r.Size < 0 || !digestPattern.MatchString(r.SHA256) {
		return errors.New("invalid object size or SHA-256")
	}
	if !validETag(r.ETag) {
		return errInvalidETag
	}
	return nil
}

func (r DeleteKeysRequest) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if len(r.Keys) == 0 || len(r.Keys) > MaxKeys {
		return errors.New("invalid delete key count")
	}
	for _, key := range r.Keys {
		if !r.Binding.validRelativeKey(key, false) {
			return errInvalidKey
		}
	}
	return nil
}

func (r ListPrefixRequest) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if !r.Binding.validRelativeKey(r.Prefix, true) || r.Limit < 1 || r.Limit > MaxPageSize {
		return errors.New("invalid list prefix or page size")
	}
	if !boundedText(r.Cursor, MaxCursorBytes, true) {
		return RPCError{Code: CodeInvalidCursor}
	}
	return nil
}

// Validate checks the page against its request, including the returned key scope.
func (r ListPrefixResult) Validate(req ListPrefixRequest) error {
	if r.Keys == nil || len(r.Keys) > req.Limit || len(r.Keys) > MaxPageSize {
		return errors.New("invalid list result size")
	}
	if !boundedText(r.NextCursor, MaxCursorBytes, true) || (r.NextCursor != "" && r.NextCursor == req.Cursor) {
		return errors.New("invalid next cursor")
	}
	seen := make(map[string]struct{}, len(r.Keys))
	for _, key := range r.Keys {
		if !req.Binding.validRelativeKey(key, false) || !strings.HasPrefix(key, req.Prefix) {
			return errors.New("list result key is outside the requested prefix")
		}
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate list result key")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (b Binding) validRelativeKey(key string, prefix bool) bool {
	return len(b.Prefix)+len(key) <= MaxKeyBytes && validKey(key, prefix)
}

func validKey(key string, prefix bool) bool {
	if key == "" {
		return prefix
	}
	if !boundedText(key, MaxKeyBytes, false) || strings.ContainsRune(key, '\\') {
		return false
	}
	if prefix && strings.HasSuffix(key, "/") {
		key = strings.TrimSuffix(key, "/")
	}
	for _, component := range strings.Split(key, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func boundedText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func jsonObject(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] == '{' && utf8.Valid(raw) && json.Valid(raw)
}

func validMediaType(value string) bool {
	if !boundedText(value, MaxContentTypeBytes, false) {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.ContainsRune(mediaType, '/') && !strings.ContainsRune(mediaType, '*')
}
