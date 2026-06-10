package usage

import "testing"

func TestRingBuffer_PushAndSnapshot(t *testing.T) {
	rb := NewRingBuffer[int](3)
	if rb.Len() != 0 || rb.Cap() != 3 {
		t.Fatalf("initial len=%d cap=%d", rb.Len(), rb.Cap())
	}

	rb.Push(10)
	rb.Push(20)
	if rb.Len() != 2 {
		t.Fatalf("expected len=2, got %d", rb.Len())
	}

	snap := rb.Snapshot()
	if len(snap) != 2 || snap[0] != 10 || snap[1] != 20 {
		t.Fatalf("unexpected snapshot: %v", snap)
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := NewRingBuffer[int](2)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3) // overwrites 1

	if rb.Len() != 2 {
		t.Fatalf("expected len=2, got %d", rb.Len())
	}
	snap := rb.Snapshot()
	if len(snap) != 2 || snap[0] != 2 || snap[1] != 3 {
		t.Fatalf("expected [2,3], got %v", snap)
	}
}

func TestRingBuffer_SnapshotIsCopy(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(100)
	snap := rb.Snapshot()
	snap[0] = 999
	if v := rb.Snapshot()[0]; v != 100 {
		t.Fatalf("snapshot must be independent: %d", v)
	}
}

func TestRingBuffer_CapacityOne(t *testing.T) {
	rb := NewRingBuffer[int](1)
	rb.Push(7)
	rb.Push(8)
	if rb.Len() != 1 || rb.Snapshot()[0] != 8 {
		t.Fatal("capacity-1 ring should keep only latest")
	}
}

func TestRingBuffer_ZeroCapClamped(t *testing.T) {
	rb := NewRingBuffer[int](0)
	if rb.Cap() != 1 {
		t.Fatalf("zero cap should be clamped to 1, got %d", rb.Cap())
	}
}

func TestRingBuffer_StructPush(t *testing.T) {
	type item struct{ ID int }
	rb := NewRingBuffer[item](2)
	rb.Push(item{1})
	rb.Push(item{2})
	rb.Push(item{3})
	snap := rb.Snapshot()
	if snap[0].ID != 2 || snap[1].ID != 3 {
		t.Fatalf("struct ring: %v", snap)
	}
}
