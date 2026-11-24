package storagedriver

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxRangeBytes = 64
	entityTag     = `(?:W/)?"[!#-~]*"`
)

var (
	etagPattern        = regexp.MustCompile(`^` + entityTag + `$`)
	etagTokenPattern   = regexp.MustCompile(entityTag)
	ifNoneMatchPattern = regexp.MustCompile(`^[ \t]*(?:\*|` + entityTag + `(?:[ \t]*,[ \t]*` + entityTag + `)*)[ \t]*$`)
	byteRangePattern   = regexp.MustCompile(`^bytes=([0-9]*)-([0-9]*)$`)
)

func (r OpenObjectRequest) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	if !r.Binding.validRelativeKey(r.Key, false) {
		return errInvalidKey
	}
	if !validIfNoneMatch(r.IfNoneMatch) {
		return errors.New("invalid If-None-Match")
	}
	if r.Range == "" {
		return nil
	}
	_, _, err := parseByteRange(r.Range)
	return err
}

// MatchesIfNoneMatch reports whether a valid condition matches a valid ETag by
// the HTTP weak comparison function. Call only after finding the object.
func MatchesIfNoneMatch(condition, etag string) bool {
	if !validETag(etag) || !validIfNoneMatch(condition) {
		return false
	}
	if strings.TrimSpace(condition) == "*" {
		return true
	}
	for _, candidate := range etagTokenPattern.FindAllString(condition, -1) {
		if strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

func (r OpenObjectResult) Validate(req OpenObjectRequest) error {
	if !validETag(r.ETag) {
		return errInvalidETag
	}
	if r.NotModified != MatchesIfNoneMatch(req.IfNoneMatch, r.ETag) {
		return errors.New("invalid conditional open result")
	}
	if r.NotModified {
		if r.Body != nil || r.ContentType != "" || r.ContentRange != nil || r.ContentLength != 0 {
			return errors.New("304 must carry only the ETag")
		}
		return nil
	}
	if r.Body == nil || r.ContentLength < 0 || !validMediaType(r.ContentType) {
		return errors.New("invalid open body or content metadata")
	}
	if req.Range == "" && r.ContentRange == nil {
		return nil
	}
	if req.Range == "" || r.ContentRange == nil {
		return errors.New("invalid range result")
	}
	resolved, err := ResolveByteRange(req.Range, r.ContentRange.Size)
	if err != nil || *resolved != *r.ContentRange || r.ContentLength != resolved.End-resolved.Start+1 {
		return errors.New("invalid range result")
	}
	return nil
}

// ResolveByteRange normalizes a single HTTP byte range against the full object size.
// An absent Range returns nil. An invalid range returns invalid_request and a valid
// range with no bytes in the representation returns range_not_satisfiable.
func ResolveByteRange(value string, size int64) (*ContentRange, error) {
	if size < 0 {
		return nil, RPCError{Code: CodeInvalidRequest}
	}
	if value == "" {
		return nil, nil
	}
	start, end, err := parseByteRange(value)
	if err != nil {
		return nil, err
	}
	if size == 0 || start >= size || (start < 0 && end == 0) {
		return nil, RPCError{Code: CodeRangeNotSatisfiable}
	}
	if start < 0 {
		start = max(size-end, 0)
		end = size - 1
	} else if end < 0 || end >= size {
		end = size - 1
	}
	return &ContentRange{Start: start, End: end, Size: size}, nil
}

func parseByteRange(value string) (start, end int64, err error) {
	invalid := RPCError{Code: CodeInvalidRequest}
	if len(value) > MaxRangeBytes {
		return 0, 0, invalid
	}
	match := byteRangePattern.FindStringSubmatch(value)
	if match == nil || (match[1] == "" && match[2] == "") {
		return 0, 0, invalid
	}
	start, end = -1, -1
	if match[1] != "" {
		start, err = strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return 0, 0, invalid
		}
	}
	if match[2] != "" {
		end, err = strconv.ParseInt(match[2], 10, 64)
		if err != nil || (start >= 0 && end < start) {
			return 0, 0, invalid
		}
	}
	return start, end, nil
}

func validETag(value string) bool {
	return len(value) <= MaxETagBytes && etagPattern.MatchString(value)
}

func validIfNoneMatch(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > MaxIfNoneMatchBytes || !ifNoneMatchPattern.MatchString(value) {
		return false
	}
	for _, tag := range etagTokenPattern.FindAllString(value, -1) {
		if len(tag) > MaxETagBytes {
			return false
		}
	}
	return true
}
