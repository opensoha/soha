package networkprotocol

import (
	"encoding/base64"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DecodeRadiusAttachment decodes an attribute escaped for the REST JSON bridge.
func DecodeRadiusAttachment(value string, maxBytes int) (string, bool) {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	return string(raw), err == nil && len(raw) <= maxBytes && utf8.Valid(raw) && !strings.ContainsRune(string(raw), 0)
}

func (r *RadiusAccountingRequest) NormalizeNASPort() bool {
	if r.NASPortIDBase64 != "" {
		port, valid := DecodeRadiusAttachment(r.NASPortIDBase64, 128)
		if !valid || r.NASPortID != "" {
			return false
		}
		r.NASPortID = port
	} else if r.NASPortID == "" && r.NASPort != nil {
		r.NASPortID = strconv.FormatInt(*r.NASPort, 10)
	}
	r.NASPortIDBase64, r.NASPort = "", nil
	return true
}

// NormalizeOctets converts RADIUS high/low words to signed 64-bit cumulative
// snapshots and keeps already-normalized counters from older API clients.
func (r *RadiusAccountingRequest) NormalizeOctets() bool {
	input, inputValid := radiusOctets(r.InputOctets, r.InputGigawords)
	output, outputValid := radiusOctets(r.OutputOctets, r.OutputGigawords)
	if !inputValid || !outputValid {
		return false
	}
	r.InputOctets, r.OutputOctets = input, output
	r.InputGigawords, r.OutputGigawords = nil, nil
	return true
}

func radiusOctets(octets int64, gigawords *int64) (int64, bool) {
	if gigawords == nil {
		return octets, octets >= 0
	}
	if octets < 0 || octets > math.MaxUint32 || *gigawords < 0 || *gigawords > math.MaxInt32 {
		return 0, false
	}
	return *gigawords<<32 | octets, true
}
