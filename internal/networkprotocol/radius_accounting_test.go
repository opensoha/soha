package networkprotocol

import (
	"math"
	"testing"
)

func TestRadiusAccountingOctets(t *testing.T) {
	high := int64(math.MaxInt32)
	packet := RadiusAccountingRequest{InputGigawords: &high, RadiusAccounting: RadiusAccounting{InputOctets: math.MaxUint32, OutputOctets: 42}}
	if !packet.NormalizeOctets() {
		t.Fatalf("counter normalization = %#v", packet)
	}
	if !packet.NormalizeOctets() || packet.InputOctets != math.MaxInt64 || packet.OutputOctets != 42 {
		t.Fatalf("repeated counter normalization = %#v", packet)
	}
	high++
	packet.OutputGigawords = &high
	if packet.NormalizeOctets() {
		t.Fatal("accepted counter outside signed storage range")
	}
}
