package kubernetes

import (
	"math"
	"testing"
)

func TestResourceCountAndGatewayInt32Bounds(t *testing.T) {
	for _, value := range []int{-1, 0, 1, math.MaxInt32, math.MaxInt32 + 1} {
		want := int32(0)
		if value >= 0 && value <= math.MaxInt32 {
			want = int32(value)
		}
		if got := resourceCount(value); got != want {
			t.Fatalf("resourceCount(%d)=%d, want %d", value, got, want)
		}
	}
	for _, value := range []int64{math.MinInt32 - 1, math.MinInt32, -1, 0, 1, math.MaxInt32, math.MaxInt32 + 1} {
		want := int32(0)
		if value >= math.MinInt32 && value <= math.MaxInt32 {
			want = int32(value)
		}
		if got := gatewayInt32(value); got != want {
			t.Fatalf("gatewayInt32(%d)=%d, want %d", value, got, want)
		}
	}
}
