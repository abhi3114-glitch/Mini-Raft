package raft

import (
	"sync"
	"time"
)

// NodeState represents the current state of a Raft node
type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// LogEntry represents a single entry in the Raft log
type LogEntry struct {
	Index   uint64 // Log index (1-based)
	Term    uint64 // Term when entry was created
	Command []byte // Command to apply to state machine
}

// Config holds the configuration for a Raft node
type Config struct {
	ID                string        // Unique identifier for this node
	Peers             []string      // Addresses of peer nodes
	ElectionTimeout   time.Duration // Base election timeout
	HeartbeatInterval time.Duration // Heartbeat interval for leader
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig(id string, peers []string) Config {
	return Config{
		ID:                id,
		Peers:             peers,
		ElectionTimeout:   150 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
	}
}

// PersistentState holds state that must survive restarts
type PersistentState struct {
	mu          sync.RWMutex
	CurrentTerm uint64   // Latest term server has seen
	VotedFor    string   // CandidateId that received vote in current term (empty if none)
	Log         []LogEntry // Log entries
}

// VolatileState holds state that can be reconstructed after restart
type VolatileState struct {
	CommitIndex uint64 // Index of highest log entry known to be committed
	LastApplied uint64 // Index of highest log entry applied to state machine
}

// LeaderState holds volatile state on leaders (reinitialized after election)
type LeaderState struct {
	NextIndex  map[string]uint64 // For each server, index of next log entry to send
	MatchIndex map[string]uint64 // For each server, index of highest log entry known to be replicated
}

// RequestVoteRequest is sent by candidates to gather votes
type RequestVoteRequest struct {
	Term         uint64 // Candidate's term
	CandidateID  string // Candidate requesting vote
	LastLogIndex uint64 // Index of candidate's last log entry
	LastLogTerm  uint64 // Term of candidate's last log entry
}

// RequestVoteResponse is the response to a RequestVote RPC
type RequestVoteResponse struct {
	Term        uint64 // CurrentTerm, for candidate to update itself
	VoteGranted bool   // True means candidate received vote
}

// AppendEntriesRequest is sent by leader to replicate log entries and as heartbeat
type AppendEntriesRequest struct {
	Term         uint64     // Leader's term
	LeaderID     string     // So follower can redirect clients
	PrevLogIndex uint64     // Index of log entry immediately preceding new ones
	PrevLogTerm  uint64     // Term of PrevLogIndex entry
	Entries      []LogEntry // Log entries to store (empty for heartbeat)
	LeaderCommit uint64     // Leader's commitIndex
}

// AppendEntriesResponse is the response to an AppendEntries RPC
type AppendEntriesResponse struct {
	Term    uint64 // CurrentTerm, for leader to update itself
	Success bool   // True if follower contained entry matching PrevLogIndex and PrevLogTerm

	// Optimization: help leader find correct NextIndex faster
	ConflictIndex uint64 // First index where logs diverge
	ConflictTerm  uint64 // Term of the conflicting entry (if any)
}

// ApplyMsg is sent to the state machine when entries are committed
type ApplyMsg struct {
	CommandValid bool
	Command      []byte
	CommandIndex uint64
	CommandTerm  uint64
}
