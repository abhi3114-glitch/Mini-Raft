package raft

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// Node represents a single Raft node
type Node struct {
	mu sync.RWMutex

	// Identity
	id    string
	peers []string

	// Configuration
	config Config

	// Persistent state
	currentTerm uint64
	votedFor    string
	log         *Log

	// Volatile state
	commitIndex uint64
	lastApplied uint64

	// Leader state (reinitialized after election)
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Current state
	state NodeState

	// Channels and timers
	applyCh         chan ApplyMsg
	electionTimer   *time.Timer
	heartbeatTicker *time.Ticker

	// Transport for RPC
	transport Transport

	// State machine
	stateMachine StateMachine

	// Control
	stopCh chan struct{}
	wg     sync.WaitGroup

	// Logger
	logger *log.Logger
}

// NewNode creates a new Raft node
func NewNode(config Config, transport Transport, stateMachine StateMachine, logger *log.Logger) *Node {
	if logger == nil {
		logger = log.Default()
	}

	n := &Node{
		id:           config.ID,
		peers:        config.Peers,
		config:       config,
		currentTerm:  0,
		votedFor:     "",
		log:          NewLog(),
		commitIndex:  0,
		lastApplied:  0,
		state:        Follower,
		applyCh:      make(chan ApplyMsg, 100),
		transport:    transport,
		stateMachine: stateMachine,
		stopCh:       make(chan struct{}),
		logger:       logger,
	}

	return n
}

// Start starts the Raft node
func (n *Node) Start() {
	n.mu.Lock()
	n.state = Follower
	n.stopCh = make(chan struct{}) // Reinitialize for restart capability
	n.resetElectionTimer()
	n.mu.Unlock()

	// Start the main loop
	n.wg.Add(1)
	go n.run()

	// Start the apply loop
	n.wg.Add(1)
	go n.applyLoop()

	n.logger.Printf("[%s] Node started as Follower", n.id)
}

// Stop stops the Raft node
func (n *Node) Stop() {
	close(n.stopCh)
	n.wg.Wait()

	n.mu.Lock()
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
	}
	n.mu.Unlock()

	n.logger.Printf("[%s] Node stopped", n.id)
}

// run is the main event loop
func (n *Node) run() {
	defer n.wg.Done()

	for {
		select {
		case <-n.stopCh:
			return
		default:
		}

		n.mu.RLock()
		state := n.state
		n.mu.RUnlock()

		switch state {
		case Follower:
			n.runFollower()
		case Candidate:
			n.runCandidate()
		case Leader:
			n.runLeader()
		}
	}
}

// runFollower handles follower state
func (n *Node) runFollower() {
	n.mu.RLock()
	timer := n.electionTimer
	n.mu.RUnlock()

	select {
	case <-n.stopCh:
		return
	case <-timer.C:
		n.logger.Printf("[%s] Election timeout, becoming candidate", n.id)
		n.mu.Lock()
		n.state = Candidate
		n.mu.Unlock()
	}
}

// runCandidate handles candidate state
func (n *Node) runCandidate() {
	n.mu.Lock()
	n.currentTerm++
	n.votedFor = n.id
	term := n.currentTerm
	lastLogIndex := n.log.LastIndex()
	lastLogTerm := n.log.LastTerm()
	n.resetElectionTimer()
	n.mu.Unlock()

	n.logger.Printf("[%s] Starting election for term %d", n.id, term)

	// Vote for self
	votesReceived := 1
	votesNeeded := (len(n.peers)+1)/2 + 1

	// Request votes from all peers
	var votesMu sync.Mutex
	var wg sync.WaitGroup

	for _, peer := range n.peers {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()

			req := RequestVoteRequest{
				Term:         term,
				CandidateID:  n.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}

			resp, err := n.transport.RequestVote(peer, req)
			if err != nil {
				n.logger.Printf("[%s] Failed to request vote from %s: %v", n.id, peer, err)
				return
			}

			n.mu.Lock()
			defer n.mu.Unlock()

			// Check if we're still a candidate
			if n.state != Candidate || n.currentTerm != term {
				return
			}

			// If response term is higher, become follower
			if resp.Term > n.currentTerm {
				n.currentTerm = resp.Term
				n.state = Follower
				n.votedFor = ""
				return
			}

			if resp.VoteGranted {
				votesMu.Lock()
				votesReceived++
				votes := votesReceived
				votesMu.Unlock()

				if votes >= votesNeeded {
					n.logger.Printf("[%s] Won election for term %d with %d votes", n.id, term, votes)
					n.becomeLeader()
				}
			}
		}(peer)
	}

	// Wait for votes or timeout, checking periodically if we became leader
	for {
		n.mu.RLock()
		state := n.state
		timer := n.electionTimer
		n.mu.RUnlock()

		// Exit if we're no longer a candidate
		if state != Candidate {
			return
		}

		select {
		case <-n.stopCh:
			return
		case <-timer.C:
			n.mu.Lock()
			if n.state == Candidate {
				n.logger.Printf("[%s] Election timeout, restarting election", n.id)
				// State remains Candidate, will start new election
			}
			n.mu.Unlock()
			return
		default:
			// Check periodically
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// runLeader handles leader state
func (n *Node) runLeader() {
	n.mu.RLock()
	ticker := n.heartbeatTicker
	n.mu.RUnlock()

	if ticker == nil {
		return
	}

	select {
	case <-n.stopCh:
		return
	case <-ticker.C:
		n.sendHeartbeats()
	}
}

// becomeLeader transitions to leader state
// Must be called with lock held
func (n *Node) becomeLeader() {
	n.state = Leader

	// Initialize leader state
	n.nextIndex = make(map[string]uint64)
	n.matchIndex = make(map[string]uint64)

	lastLogIndex := n.log.LastIndex()
	for _, peer := range n.peers {
		n.nextIndex[peer] = lastLogIndex + 1
		n.matchIndex[peer] = 0
	}

	// Stop election timer
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}

	// Start heartbeat ticker
	n.heartbeatTicker = time.NewTicker(n.config.HeartbeatInterval)

	n.logger.Printf("[%s] Became leader for term %d", n.id, n.currentTerm)

	// Send initial heartbeats
	go n.sendHeartbeats()
}

// sendHeartbeats sends AppendEntries RPCs to all peers
func (n *Node) sendHeartbeats() {
	n.mu.RLock()
	if n.state != Leader {
		n.mu.RUnlock()
		return
	}
	term := n.currentTerm
	leaderCommit := n.commitIndex
	n.mu.RUnlock()

	n.logger.Printf("[%s] Sending heartbeats with leaderCommit=%d", n.id, leaderCommit)

	for _, peer := range n.peers {
		go n.replicateTo(peer, term, leaderCommit)
	}
}

// replicateTo sends AppendEntries to a specific peer
func (n *Node) replicateTo(peer string, term uint64, leaderCommit uint64) {
	n.mu.RLock()
	if n.state != Leader {
		n.mu.RUnlock()
		return
	}

	nextIdx := n.nextIndex[peer]
	prevLogIndex := nextIdx - 1
	prevLogTerm := n.log.TermAt(prevLogIndex)

	var entries []LogEntry
	if n.log.LastIndex() >= nextIdx {
		entries = n.log.GetFrom(nextIdx)
	}
	n.mu.RUnlock()

	req := AppendEntriesRequest{
		Term:         term,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: leaderCommit,
	}

	resp, err := n.transport.AppendEntries(peer, req)
	if err != nil {
		n.logger.Printf("[%s] Failed to send AppendEntries to %s: %v", n.id, peer, err)
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != Leader || n.currentTerm != term {
		return
	}

	if resp.Term > n.currentTerm {
		n.currentTerm = resp.Term
		n.state = Follower
		n.votedFor = ""
		if n.heartbeatTicker != nil {
			n.heartbeatTicker.Stop()
		}
		n.resetElectionTimer()
		return
	}

	if resp.Success {
		// Update nextIndex and matchIndex
		if len(entries) > 0 {
			newMatchIndex := entries[len(entries)-1].Index
			n.nextIndex[peer] = newMatchIndex + 1
			n.matchIndex[peer] = newMatchIndex
			n.logger.Printf("[%s] Replication to %s succeeded, matchIndex=%d", n.id, peer, newMatchIndex)
			n.updateCommitIndex()
		}
	} else {
		// Decrement nextIndex and retry
		if resp.ConflictIndex > 0 {
			n.nextIndex[peer] = resp.ConflictIndex
		} else if n.nextIndex[peer] > 1 {
			n.nextIndex[peer]--
		}
		n.logger.Printf("[%s] Replication to %s failed, new nextIndex=%d", n.id, peer, n.nextIndex[peer])
	}
}

// resetElectionTimer resets the election timer with randomized timeout
func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}

	// Randomize timeout between 1x and 2x of base timeout
	timeout := n.config.ElectionTimeout + time.Duration(rand.Int63n(int64(n.config.ElectionTimeout)))
	n.electionTimer = time.NewTimer(timeout)
}

// HandleRequestVote handles an incoming RequestVote RPC
func (n *Node) HandleRequestVote(req RequestVoteRequest) RequestVoteResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := RequestVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: false,
	}

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		return resp
	}

	// If RPC term is greater, update current term and become follower
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.state = Follower
		n.votedFor = ""
		if n.heartbeatTicker != nil {
			n.heartbeatTicker.Stop()
		}
	}

	resp.Term = n.currentTerm

	// Check if we can vote for this candidate
	if (n.votedFor == "" || n.votedFor == req.CandidateID) && n.isLogUpToDate(req.LastLogIndex, req.LastLogTerm) {
		n.votedFor = req.CandidateID
		resp.VoteGranted = true
		n.resetElectionTimer()
		n.logger.Printf("[%s] Voted for %s in term %d", n.id, req.CandidateID, n.currentTerm)
	}

	return resp
}

// isLogUpToDate checks if candidate's log is at least as up-to-date as ours
func (n *Node) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastIndex := n.log.LastIndex()
	myLastTerm := n.log.LastTerm()

	// Compare by term first, then by index
	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}
	return lastLogIndex >= myLastIndex
}

// HandleAppendEntries handles an incoming AppendEntries RPC
func (n *Node) HandleAppendEntries(req AppendEntriesRequest) AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := AppendEntriesResponse{
		Term:    n.currentTerm,
		Success: false,
	}

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		return resp
	}

	// Reset election timer (we received valid RPC from leader)
	n.resetElectionTimer()

	// If RPC term is greater or equal, recognize the leader
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
	}

	// Step down if we were candidate or leader
	if n.state != Follower {
		n.state = Follower
		if n.heartbeatTicker != nil {
			n.heartbeatTicker.Stop()
		}
	}

	resp.Term = n.currentTerm

	// Check log consistency
	if !n.log.MatchesPrevLog(req.PrevLogIndex, req.PrevLogTerm) {
		n.logger.Printf("[%s] Log mismatch at prevLogIndex=%d, prevLogTerm=%d, myLastIndex=%d",
			n.id, req.PrevLogIndex, req.PrevLogTerm, n.log.LastIndex())
		// Find conflict info for fast backup
		if req.PrevLogIndex > n.log.LastIndex() {
			resp.ConflictIndex = n.log.LastIndex() + 1
			resp.ConflictTerm = 0
		} else {
			resp.ConflictTerm = n.log.TermAt(req.PrevLogIndex)
			// Find first index of conflicting term
			resp.ConflictIndex = req.PrevLogIndex
			for i := req.PrevLogIndex - 1; i > 0; i-- {
				if n.log.TermAt(i) != resp.ConflictTerm {
					break
				}
				resp.ConflictIndex = i
			}
		}
		return resp
	}

	// Append new entries
	if len(req.Entries) > 0 {
		n.log.AppendAfter(req.PrevLogIndex, req.Entries)
		n.logger.Printf("[%s] Appended %d entries after index %d", n.id, len(req.Entries), req.PrevLogIndex)
	}

	// Update commit index
	if req.LeaderCommit > n.commitIndex {
		lastNewEntry := req.PrevLogIndex + uint64(len(req.Entries))
		if lastNewEntry == 0 {
			lastNewEntry = n.log.LastIndex()
		}
		if req.LeaderCommit < lastNewEntry {
			n.commitIndex = req.LeaderCommit
		} else {
			n.commitIndex = lastNewEntry
		}
		n.logger.Printf("[%s] Updated commit index to %d (leaderCommit=%d)", n.id, n.commitIndex, req.LeaderCommit)
	}

	resp.Success = true
	return resp
}

// updateCommitIndex updates the commit index based on matchIndex values
// Must be called with lock held
func (n *Node) updateCommitIndex() {
	// Find the highest index that has been replicated to a majority
	for idx := n.log.LastIndex(); idx > n.commitIndex; idx-- {
		// Only commit entries from current term
		if n.log.TermAt(idx) != n.currentTerm {
			continue
		}

		// Count replicas
		count := 1 // Self
		for _, matchIdx := range n.matchIndex {
			if matchIdx >= idx {
				count++
			}
		}

		// Check majority
		if count > (len(n.peers)+1)/2 {
			n.commitIndex = idx
			n.logger.Printf("[%s] Updated commit index to %d", n.id, idx)
			break
		}
	}
}

// applyLoop applies committed entries to the state machine
func (n *Node) applyLoop() {
	defer n.wg.Done()

	for {
		select {
		case <-n.stopCh:
			return
		default:
		}

		n.mu.Lock()
		commitIndex := n.commitIndex
		lastApplied := n.lastApplied
		n.mu.Unlock()

		for lastApplied < commitIndex {
			lastApplied++
			entry := n.log.Get(lastApplied)
			if entry == nil {
				continue
			}

			// Apply to state machine
			if n.stateMachine != nil {
				n.stateMachine.Apply(entry.Command)
				n.logger.Printf("[%s] Applied entry %d to state machine", n.id, lastApplied)
			}

			// Send to apply channel
			msg := ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
				CommandTerm:  entry.Term,
			}

			select {
			case n.applyCh <- msg:
			case <-n.stopCh:
				return
			}

			n.mu.Lock()
			n.lastApplied = lastApplied
			n.mu.Unlock()
		}

		// Small sleep to prevent busy loop
		time.Sleep(10 * time.Millisecond)
	}
}

// Submit submits a command to the Raft cluster
// Returns the index, term, and whether this node is the leader
func (n *Node) Submit(command []byte) (index uint64, term uint64, isLeader bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != Leader {
		return 0, 0, false
	}

	index = n.log.LastIndex() + 1
	term = n.currentTerm

	entry := LogEntry{
		Index:   index,
		Term:    term,
		Command: command,
	}

	n.log.Append(entry)
	n.logger.Printf("[%s] Appended entry at index %d, term %d", n.id, index, term)

	// Trigger immediate replication
	go n.sendHeartbeats()

	return index, term, true
}

// State returns the current state of the node
func (n *Node) State() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state
}

// Term returns the current term
func (n *Node) Term() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.currentTerm
}

// ApplyCh returns the channel for applied commands
func (n *Node) ApplyCh() <-chan ApplyMsg {
	return n.applyCh
}

// ID returns the node ID
func (n *Node) ID() string {
	return n.id
}

// IsLeader returns whether this node is the leader
func (n *Node) IsLeader() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state == Leader
}

// LeaderID returns the ID of the current leader (if known)
// Returns empty string if leader is unknown
func (n *Node) LeaderID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.state == Leader {
		return n.id
	}
	// Followers don't track leader ID in this simple implementation
	return ""
}
