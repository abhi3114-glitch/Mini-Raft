# Mini-Raft

A focused implementation of the Raft consensus algorithm in Go, covering leader election, log replication, commit index tracking, and state machine execution.

## What is Raft?

Raft is a consensus algorithm designed for distributed systems. It allows a cluster of servers to agree on a sequence of commands, even when some servers fail. Raft breaks consensus into three sub-problems:

1. **Leader Election** - One server is elected as leader to coordinate the cluster
2. **Log Replication** - The leader receives commands and replicates them to followers
3. **Safety** - Committed commands are never lost, even during failures

## Features

This implementation includes:

- **Leader Election**: Randomized election timeouts prevent split votes. Candidates request votes from peers and become leader upon receiving a majority.

- **Log Replication**: Leaders append commands to their log and replicate entries to followers using AppendEntries RPCs. Consistency checks ensure logs remain identical across nodes.

- **Commit Index**: Leaders track which entries have been replicated to a majority and propagate commit information to followers via heartbeats.

- **State Machine**: A simple key-value store demonstrates how committed commands are applied. Supports SET, GET, and DELETE operations.

## Project Structure

```
Mini-Raft/
├── go.mod                 # Go module definition
├── raft/
│   ├── types.go           # NodeState, LogEntry, RPC message types
│   ├── node.go            # Main Raft node implementation
│   ├── log.go             # Log management and conflict detection
│   ├── statemachine.go    # StateMachine interface and KV store
│   ├── transport.go       # In-memory and TCP transport layers
│   └── raft_test.go       # Comprehensive test suite
└── cmd/demo/main.go       # Interactive 3-node cluster demo
```

## Getting Started

### Prerequisites

- Go 1.21 or later

### Run Tests

```bash
cd Mini-Raft
go test ./... -v
```

### Run Interactive Demo

```bash
go run ./cmd/demo
```

The demo starts a 3-node Raft cluster. Available commands:

| Command | Description |
|---------|-------------|
| `SET key value` | Store a key-value pair |
| `GET key` | Retrieve a value from all nodes |
| `STATUS` | Display cluster state (leader, terms) |
| `STOP node` | Simulate node failure |
| `START node` | Restart a stopped node |
| `QUIT` | Exit the demo |

## How It Works

### Election Process

1. Nodes start as followers with randomized election timeouts
2. If a follower receives no heartbeat before timeout, it becomes a candidate
3. Candidate increments term, votes for itself, and requests votes from peers
4. Upon receiving majority votes, candidate becomes leader
5. Leader sends periodic heartbeats to maintain authority

### Replication Process

1. Client submits command to leader
2. Leader appends command to its log
3. Leader sends AppendEntries RPC to all followers
4. Followers append entry if log consistency check passes
5. Once majority acknowledges, leader commits the entry
6. Leader includes commit index in subsequent heartbeats
7. Followers apply committed entries to their state machines

### Consistency Guarantees

- **Election Safety**: At most one leader per term
- **Leader Append-Only**: Leaders never overwrite or delete log entries
- **Log Matching**: If two logs contain an entry with the same index and term, all preceding entries are identical
- **Leader Completeness**: Committed entries will appear in all future leaders' logs

## Configuration

Key configuration options in `raft.Config`:

| Option | Default | Description |
|--------|---------|-------------|
| `ElectionTimeout` | 150ms | Base timeout before starting election |
| `HeartbeatInterval` | 50ms | Interval between leader heartbeats |

Election timeout is randomized between 1x and 2x the base value to prevent split votes.

## API Reference

### Creating a Node

```go
config := raft.Config{
    ID:                "node1",
    Peers:             []string{"node2", "node3"},
    ElectionTimeout:   150 * time.Millisecond,
    HeartbeatInterval: 50 * time.Millisecond,
}

transport := raft.NewTCPTransport("localhost:8001", 5*time.Second)
stateMachine := raft.NewKVStateMachine()
node := raft.NewNode(config, transport, stateMachine, logger)

transport.Listen(node)
node.Start()
```

### Submitting Commands

```go
index, term, isLeader := node.Submit([]byte("SET foo bar"))
if !isLeader {
    // Redirect to leader
}
```

### Checking State

```go
state := node.State()      // Follower, Candidate, or Leader
term := node.Term()        // Current term
isLeader := node.IsLeader()
```

## Testing

The test suite covers:

- Log operations (append, truncate, conflict detection)
- State machine commands (SET, GET, DELETE)
- Transport layer (memory and TCP)
- Leader election across a 3-node cluster
- Log replication and commit propagation

Run specific tests:

```bash
go test ./raft -run TestLogReplication -v
go test ./raft -run TestNodeStateTransitions -v
```

## Limitations

This is an educational implementation. Production use would require:

- Persistent storage for log and state
- Snapshot support for log compaction
- Membership changes (adding/removing nodes)
- Client session tracking for linearizable reads

## References

- [The Raft Consensus Algorithm](https://raft.github.io/)
- [In Search of an Understandable Consensus Algorithm](https://raft.github.io/raft.pdf) - Original Raft paper

## License

MIT License
