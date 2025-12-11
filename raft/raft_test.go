package raft

import (
	"log"
	"os"
	"testing"
	"time"
)

func TestLogAppendAndGet(t *testing.T) {
	log := NewLog()

	// Test empty log
	if log.LastIndex() != 0 {
		t.Errorf("Expected LastIndex 0, got %d", log.LastIndex())
	}
	if log.LastTerm() != 0 {
		t.Errorf("Expected LastTerm 0, got %d", log.LastTerm())
	}

	// Append entries
	log.Append(LogEntry{Index: 1, Term: 1, Command: []byte("cmd1")})
	log.Append(LogEntry{Index: 2, Term: 1, Command: []byte("cmd2")})
	log.Append(LogEntry{Index: 3, Term: 2, Command: []byte("cmd3")})

	// Test LastIndex and LastTerm
	if log.LastIndex() != 3 {
		t.Errorf("Expected LastIndex 3, got %d", log.LastIndex())
	}
	if log.LastTerm() != 2 {
		t.Errorf("Expected LastTerm 2, got %d", log.LastTerm())
	}

	// Test Get
	entry := log.Get(2)
	if entry == nil {
		t.Fatal("Expected entry at index 2")
	}
	if string(entry.Command) != "cmd2" {
		t.Errorf("Expected command 'cmd2', got '%s'", string(entry.Command))
	}

	// Test out of bounds
	if log.Get(0) != nil {
		t.Error("Expected nil for index 0")
	}
	if log.Get(10) != nil {
		t.Error("Expected nil for index 10")
	}
}

func TestLogTruncate(t *testing.T) {
	log := NewLog()

	log.Append(LogEntry{Index: 1, Term: 1, Command: []byte("cmd1")})
	log.Append(LogEntry{Index: 2, Term: 1, Command: []byte("cmd2")})
	log.Append(LogEntry{Index: 3, Term: 2, Command: []byte("cmd3")})

	log.TruncateFrom(2)

	if log.LastIndex() != 1 {
		t.Errorf("Expected LastIndex 1 after truncate, got %d", log.LastIndex())
	}
}

func TestLogMatchesPrevLog(t *testing.T) {
	log := NewLog()

	// Empty log should match prevLogIndex 0
	if !log.MatchesPrevLog(0, 0) {
		t.Error("Empty log should match prevLogIndex 0")
	}

	log.Append(LogEntry{Index: 1, Term: 1, Command: []byte("cmd1")})
	log.Append(LogEntry{Index: 2, Term: 2, Command: []byte("cmd2")})

	// Should match existing entries
	if !log.MatchesPrevLog(1, 1) {
		t.Error("Should match prevLogIndex 1, term 1")
	}
	if !log.MatchesPrevLog(2, 2) {
		t.Error("Should match prevLogIndex 2, term 2")
	}

	// Should not match wrong term
	if log.MatchesPrevLog(1, 2) {
		t.Error("Should not match prevLogIndex 1 with wrong term 2")
	}

	// Should not match beyond log
	if log.MatchesPrevLog(5, 1) {
		t.Error("Should not match beyond log")
	}
}

func TestLogAppendAfter(t *testing.T) {
	log := NewLog()

	log.Append(LogEntry{Index: 1, Term: 1, Command: []byte("cmd1")})
	log.Append(LogEntry{Index: 2, Term: 1, Command: []byte("cmd2")})
	log.Append(LogEntry{Index: 3, Term: 1, Command: []byte("cmd3")})

	// Append after with conflicting entry
	newEntries := []LogEntry{
		{Index: 3, Term: 2, Command: []byte("new3")},
		{Index: 4, Term: 2, Command: []byte("new4")},
	}
	log.AppendAfter(2, newEntries)

	if log.LastIndex() != 4 {
		t.Errorf("Expected LastIndex 4, got %d", log.LastIndex())
	}

	entry := log.Get(3)
	if entry.Term != 2 {
		t.Errorf("Expected term 2 at index 3, got %d", entry.Term)
	}
	if string(entry.Command) != "new3" {
		t.Errorf("Expected command 'new3', got '%s'", string(entry.Command))
	}
}

func TestKVStateMachine(t *testing.T) {
	sm := NewKVStateMachine()

	// Test SET
	result, err := sm.Apply([]byte("SET foo bar"))
	if err != nil {
		t.Fatalf("SET failed: %v", err)
	}
	if string(result) != "OK" {
		t.Errorf("Expected 'OK', got '%s'", string(result))
	}

	// Test GET
	result, err = sm.Apply([]byte("GET foo"))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	if string(result) != "bar" {
		t.Errorf("Expected 'bar', got '%s'", string(result))
	}

	// Test GET non-existent
	result, _ = sm.Apply([]byte("GET nonexistent"))
	if string(result) != "(nil)" {
		t.Errorf("Expected '(nil)', got '%s'", string(result))
	}

	// Test DELETE
	sm.Apply([]byte("DELETE foo"))
	result, _ = sm.Apply([]byte("GET foo"))
	if string(result) != "(nil)" {
		t.Errorf("Expected '(nil)' after delete, got '%s'", string(result))
	}
}

func TestMemoryTransport(t *testing.T) {
	t1 := NewMemoryTransport("node1")
	t2 := NewMemoryTransport("node2")

	t1.Connect(t2)

	// Create a mock handler
	handler := &mockHandler{
		term: 1,
	}
	t2.Listen(handler)

	// Test RequestVote
	resp, err := t1.RequestVote("node2", RequestVoteRequest{
		Term:        1,
		CandidateID: "node1",
	})
	if err != nil {
		t.Fatalf("RequestVote failed: %v", err)
	}
	if resp.Term != 1 {
		t.Errorf("Expected term 1, got %d", resp.Term)
	}

	// Test AppendEntries
	aeResp, err := t1.AppendEntries("node2", AppendEntriesRequest{
		Term:     1,
		LeaderID: "node1",
	})
	if err != nil {
		t.Fatalf("AppendEntries failed: %v", err)
	}
	if !aeResp.Success {
		t.Error("Expected AppendEntries to succeed")
	}
}

type mockHandler struct {
	term uint64
}

func (h *mockHandler) HandleRequestVote(req RequestVoteRequest) RequestVoteResponse {
	return RequestVoteResponse{
		Term:        h.term,
		VoteGranted: true,
	}
}

func (h *mockHandler) HandleAppendEntries(req AppendEntriesRequest) AppendEntriesResponse {
	return AppendEntriesResponse{
		Term:    h.term,
		Success: true,
	}
}

func TestNodeStateTransitions(t *testing.T) {
	// Create a 3-node cluster with memory transport
	t1 := NewMemoryTransport("node1")
	t2 := NewMemoryTransport("node2")
	t3 := NewMemoryTransport("node3")

	t1.Connect(t2)
	t1.Connect(t3)
	t2.Connect(t3)

	config1 := Config{
		ID:                "node1",
		Peers:             []string{"node2", "node3"},
		ElectionTimeout:   50 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	}
	config2 := Config{
		ID:                "node2",
		Peers:             []string{"node1", "node3"},
		ElectionTimeout:   100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	}
	config3 := Config{
		ID:                "node3",
		Peers:             []string{"node1", "node2"},
		ElectionTimeout:   100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	}

	sm1 := NewKVStateMachine()
	sm2 := NewKVStateMachine()
	sm3 := NewKVStateMachine()

	node1 := NewNode(config1, t1, sm1, nil)
	node2 := NewNode(config2, t2, sm2, nil)
	node3 := NewNode(config3, t3, sm3, nil)

	t1.Listen(node1)
	t2.Listen(node2)
	t3.Listen(node3)

	// Start all nodes
	node1.Start()
	node2.Start()
	node3.Start()

	// Wait for leader election
	time.Sleep(300 * time.Millisecond)

	// At least one node should be leader
	leaders := 0
	if node1.IsLeader() {
		leaders++
	}
	if node2.IsLeader() {
		leaders++
	}
	if node3.IsLeader() {
		leaders++
	}

	if leaders == 0 {
		t.Error("Expected at least one leader")
	}
	if leaders > 1 {
		t.Errorf("Expected at most one leader, got %d", leaders)
	}

	// Stop all nodes
	node1.Stop()
	node2.Stop()
	node3.Stop()
}

func TestLogReplication(t *testing.T) {
	// Create a 3-node cluster with memory transport
	t1 := NewMemoryTransport("node1")
	t2 := NewMemoryTransport("node2")
	t3 := NewMemoryTransport("node3")

	t1.Connect(t2)
	t1.Connect(t3)
	t2.Connect(t3)

	// Use significantly different election timeouts to ensure node1 becomes leader
	config1 := Config{
		ID:                "node1",
		Peers:             []string{"node2", "node3"},
		ElectionTimeout:   50 * time.Millisecond,
		HeartbeatInterval: 15 * time.Millisecond,
	}
	config2 := Config{
		ID:                "node2",
		Peers:             []string{"node1", "node3"},
		ElectionTimeout:   500 * time.Millisecond, // Much longer to avoid interference
		HeartbeatInterval: 15 * time.Millisecond,
	}
	config3 := Config{
		ID:                "node3",
		Peers:             []string{"node1", "node2"},
		ElectionTimeout:   500 * time.Millisecond, // Much longer to avoid interference
		HeartbeatInterval: 15 * time.Millisecond,
	}

	sm1 := NewKVStateMachine()
	sm2 := NewKVStateMachine()
	sm3 := NewKVStateMachine()

	// Create loggers for debugging
	logger1 := log.New(os.Stderr, "[NODE1] ", log.Lmicroseconds)
	logger2 := log.New(os.Stderr, "[NODE2] ", log.Lmicroseconds)
	logger3 := log.New(os.Stderr, "[NODE3] ", log.Lmicroseconds)

	node1 := NewNode(config1, t1, sm1, logger1)
	node2 := NewNode(config2, t2, sm2, logger2)
	node3 := NewNode(config3, t3, sm3, logger3)

	t1.Listen(node1)
	t2.Listen(node2)
	t3.Listen(node3)

	// Start all nodes
	node1.Start()
	node2.Start()
	node3.Start()

	defer func() {
		node1.Stop()
		node2.Stop()
		node3.Stop()
	}()

	// Wait for leader election with polling
	var leader *Node
	nodes := []*Node{node1, node2, node3}
	for i := 0; i < 40; i++ {
		time.Sleep(25 * time.Millisecond)
		for _, n := range nodes {
			if n.IsLeader() {
				leader = n
				break
			}
		}
		if leader != nil {
			break
		}
	}

	if leader == nil {
		t.Fatal("No leader elected")
	}

	t.Logf("Leader elected: %s, term: %d", leader.ID(), leader.Term())

	// Submit a command
	index, term, isLeader := leader.Submit([]byte("SET key1 value1"))
	if !isLeader {
		t.Error("Leader should report isLeader=true")
	}
	if index != 1 {
		t.Errorf("Expected index 1, got %d", index)
	}
	if term == 0 {
		t.Error("Term should be > 0")
	}

	t.Logf("Submitted command at index=%d, term=%d", index, term)

	// Wait for replication with polling
	success := false
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)

		val1, ok1 := sm1.Get("key1")
		val2, ok2 := sm2.Get("key1")
		val3, ok3 := sm3.Get("key1")

		applied := 0
		if ok1 && val1 == "value1" {
			applied++
		}
		if ok2 && val2 == "value1" {
			applied++
		}
		if ok3 && val3 == "value1" {
			applied++
		}

		if applied >= 2 {
			t.Logf("Replication succeeded: %d nodes applied", applied)
			success = true
			break
		}
	}

	if !success {
		t.Error("Expected at least 2 nodes to apply command within timeout")
	}
}
