package api

import "testing"

func TestPodTerminalSizeRejectsOverflow(t *testing.T) {
	queue := newPodTerminalSizeQueue()
	defer queue.Close()
	for _, dimensions := range [][2]int{{0, 10}, {-1, 10}, {10, 65536}, {65536, 10}} {
		queue.Push(dimensions[0], dimensions[1])
		if len(queue.ch) != 0 {
			t.Fatalf("invalid dimensions admitted: %v", dimensions)
		}
	}
	queue.Push(65535, 65535)
	if size := queue.Next(); size.Width != 65535 || size.Height != 65535 {
		t.Fatalf("valid boundary changed: %+v", size)
	}
}
