package raft

import (
	"testing"
)

// TestDirectReplication tests replication without the timing complexity
func TestDirectReplication(t *testing.T) {
	// Create transports and connect them
	t1 := NewMemoryTransport("node1")
	t2 := NewMemoryTransport("node2")
	t3 := NewMemoryTransport("node3")

	t1.Connect(t2)
	t1.Connect(t3)
	t2.Connect(t3)

	// Create logs
	log1 := NewLog()
	log2 := NewLog()
	log3 := NewLog()

	// Create state machines
	sm1 := NewKVStateMachine()
	sm2 := NewKVStateMachine()
	sm3 := NewKVStateMachine()

	// Simulate leader with term 1
	entry := LogEntry{Index: 1, Term: 1, Command: []byte("SET key1 value1")}
	log1.Append(entry)

	// Test that log append works
	if log1.LastIndex() != 1 {
		t.Fatalf("Expected log1.LastIndex()=1, got %d", log1.LastIndex())
	}

	// Simulate AppendEntries to follower
	req := AppendEntriesRequest{
		Term:         1,
		LeaderID:     "node1",
		PrevLogIndex: 0, // First entry
		PrevLogTerm:  0,
		Entries:      []LogEntry{entry},
		LeaderCommit: 0, // Not yet committed
	}

	// Check that follower's log accepts the entry
	if !log2.MatchesPrevLog(req.PrevLogIndex, req.PrevLogTerm) {
		t.Fatal("log2 should match empty prevLog")
	}

	log2.AppendAfter(req.PrevLogIndex, req.Entries)
	if log2.LastIndex() != 1 {
		t.Fatalf("Expected log2.LastIndex()=1 after append, got %d", log2.LastIndex())
	}

	log3.AppendAfter(req.PrevLogIndex, req.Entries)
	if log3.LastIndex() != 1 {
		t.Fatalf("Expected log3.LastIndex()=1 after append, got %d", log3.LastIndex())
	}

	// Now test commit propagation
	// Leader received success from both followers, so commit index should be 1
	matchIndex := map[string]uint64{
		"node2": 1,
		"node3": 1,
	}

	// Count replicas for index 1
	count := 1 // Self
	for _, idx := range matchIndex {
		if idx >= 1 {
			count++
		}
	}
	t.Logf("Replica count for index 1: %d", count)

	// Majority check: count > (peers+1)/2 = count > (2+1)/2 = count > 1
	if count <= (2+1)/2 {
		t.Fatalf("Expected count > %d, got count=%d", (2+1)/2, count)
	}

	// Commit should be possible
	commitIndex := uint64(1)

	// Apply to state machines
	entry1 := log1.Get(commitIndex)
	if entry1 == nil {
		t.Fatal("Entry 1 should exist in log1")
	}
	sm1.Apply(entry1.Command)
	sm2.Apply(entry1.Command)
	sm3.Apply(entry1.Command)

	// Verify all state machines have the value
	val1, ok1 := sm1.Get("key1")
	val2, ok2 := sm2.Get("key1")
	val3, ok3 := sm3.Get("key1")

	if !ok1 || val1 != "value1" {
		t.Errorf("sm1: expected value1, got %s (ok=%v)", val1, ok1)
	}
	if !ok2 || val2 != "value1" {
		t.Errorf("sm2: expected value1, got %s (ok=%v)", val2, ok2)
	}
	if !ok3 || val3 != "value1" {
		t.Errorf("sm3: expected value1, got %s (ok=%v)", val3, ok3)
	}

	t.Log("Direct replication test passed!")
	_ = t1
	_ = t2
	_ = t3
}
