package raft

import (
	"encoding/gob"
	"fmt"
	"net"
	"sync"
	"time"
)

// Transport defines the interface for RPC communication between nodes
type Transport interface {
	// RequestVote sends a RequestVote RPC to the target node
	RequestVote(target string, req RequestVoteRequest) (*RequestVoteResponse, error)

	// AppendEntries sends an AppendEntries RPC to the target node
	AppendEntries(target string, req AppendEntriesRequest) (*AppendEntriesResponse, error)

	// Listen starts listening for incoming RPCs
	Listen(handler RPCHandler) error

	// Close closes the transport
	Close() error
}

// RPCHandler handles incoming RPCs
type RPCHandler interface {
	HandleRequestVote(req RequestVoteRequest) RequestVoteResponse
	HandleAppendEntries(req AppendEntriesRequest) AppendEntriesResponse
}

// MemoryTransport is an in-memory transport for testing
type MemoryTransport struct {
	mu      sync.RWMutex
	address string
	peers   map[string]*MemoryTransport
	handler RPCHandler
}

// NewMemoryTransport creates a new in-memory transport
func NewMemoryTransport(address string) *MemoryTransport {
	return &MemoryTransport{
		address: address,
		peers:   make(map[string]*MemoryTransport),
	}
}

// Connect connects this transport to another
func (t *MemoryTransport) Connect(other *MemoryTransport) {
	t.mu.Lock()
	t.peers[other.address] = other
	t.mu.Unlock()

	other.mu.Lock()
	other.peers[t.address] = t
	other.mu.Unlock()
}

// RequestVote sends a RequestVote RPC
func (t *MemoryTransport) RequestVote(target string, req RequestVoteRequest) (*RequestVoteResponse, error) {
	t.mu.RLock()
	peer, ok := t.peers[target]
	t.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown peer: %s", target)
	}

	peer.mu.RLock()
	handler := peer.handler
	peer.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("peer %s has no handler", target)
	}

	resp := handler.HandleRequestVote(req)
	return &resp, nil
}

// AppendEntries sends an AppendEntries RPC
func (t *MemoryTransport) AppendEntries(target string, req AppendEntriesRequest) (*AppendEntriesResponse, error) {
	t.mu.RLock()
	peer, ok := t.peers[target]
	t.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown peer: %s", target)
	}

	peer.mu.RLock()
	handler := peer.handler
	peer.mu.RUnlock()

	if handler == nil {
		return nil, fmt.Errorf("peer %s has no handler", target)
	}

	resp := handler.HandleAppendEntries(req)
	return &resp, nil
}

// Listen sets the RPC handler
func (t *MemoryTransport) Listen(handler RPCHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = handler
	return nil
}

// Close closes the transport
func (t *MemoryTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Disconnect from all peers
	for addr, peer := range t.peers {
		peer.mu.Lock()
		delete(peer.peers, t.address)
		peer.mu.Unlock()
		delete(t.peers, addr)
	}

	return nil
}

// Address returns the transport address
func (t *MemoryTransport) Address() string {
	return t.address
}

// TCPTransport is a TCP-based transport for real deployment
type TCPTransport struct {
	mu       sync.RWMutex
	address  string
	listener net.Listener
	handler  RPCHandler
	timeout  time.Duration
	stopCh   chan struct{}
}

// NewTCPTransport creates a new TCP transport
func NewTCPTransport(address string, timeout time.Duration) *TCPTransport {
	return &TCPTransport{
		address: address,
		timeout: timeout,
		stopCh:  make(chan struct{}),
	}
}

// RPC type identifiers
const (
	rpcRequestVote   byte = 1
	rpcAppendEntries byte = 2
)

// RequestVote sends a RequestVote RPC over TCP
func (t *TCPTransport) RequestVote(target string, req RequestVoteRequest) (*RequestVoteResponse, error) {
	conn, err := net.DialTimeout("tcp", target, t.timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(t.timeout))

	// Send RPC type
	if _, err := conn.Write([]byte{rpcRequestVote}); err != nil {
		return nil, err
	}

	// Send request
	enc := gob.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, err
	}

	// Receive response
	var resp RequestVoteResponse
	dec := gob.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// AppendEntries sends an AppendEntries RPC over TCP
func (t *TCPTransport) AppendEntries(target string, req AppendEntriesRequest) (*AppendEntriesResponse, error) {
	conn, err := net.DialTimeout("tcp", target, t.timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(t.timeout))

	// Send RPC type
	if _, err := conn.Write([]byte{rpcAppendEntries}); err != nil {
		return nil, err
	}

	// Send request
	enc := gob.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return nil, err
	}

	// Receive response
	var resp AppendEntriesResponse
	dec := gob.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Listen starts listening for incoming RPCs
func (t *TCPTransport) Listen(handler RPCHandler) error {
	listener, err := net.Listen("tcp", t.address)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.listener = listener
	t.handler = handler
	t.mu.Unlock()

	go t.acceptLoop()
	return nil
}

// acceptLoop accepts incoming connections
func (t *TCPTransport) acceptLoop() {
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		t.mu.RLock()
		listener := t.listener
		t.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				continue
			}
		}

		go t.handleConn(conn)
	}
}

// handleConn handles an incoming connection
func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(t.timeout))

	// Read RPC type
	rpcType := make([]byte, 1)
	if _, err := conn.Read(rpcType); err != nil {
		return
	}

	t.mu.RLock()
	handler := t.handler
	t.mu.RUnlock()

	if handler == nil {
		return
	}

	dec := gob.NewDecoder(conn)
	enc := gob.NewEncoder(conn)

	switch rpcType[0] {
	case rpcRequestVote:
		var req RequestVoteRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		resp := handler.HandleRequestVote(req)
		enc.Encode(resp)

	case rpcAppendEntries:
		var req AppendEntriesRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		resp := handler.HandleAppendEntries(req)
		enc.Encode(resp)
	}
}

// Close closes the transport
func (t *TCPTransport) Close() error {
	close(t.stopCh)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.listener != nil {
		t.listener.Close()
		t.listener = nil
	}

	return nil
}

// Address returns the transport address
func (t *TCPTransport) Address() string {
	return t.address
}
