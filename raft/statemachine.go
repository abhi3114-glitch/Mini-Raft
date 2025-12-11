package raft

// StateMachine is the interface that must be implemented by the application
// to apply committed commands
type StateMachine interface {
	// Apply applies a command to the state machine and returns the result
	Apply(command []byte) (result []byte, err error)
}

// KVStateMachine is a simple key-value store implementation of StateMachine
type KVStateMachine struct {
	data map[string]string
}

// NewKVStateMachine creates a new key-value state machine
func NewKVStateMachine() *KVStateMachine {
	return &KVStateMachine{
		data: make(map[string]string),
	}
}

// Apply applies a command to the KV store
// Command format: "SET key value" or "GET key" or "DELETE key"
func (kv *KVStateMachine) Apply(command []byte) ([]byte, error) {
	cmd := string(command)
	parts := splitCommand(cmd)

	if len(parts) == 0 {
		return nil, nil
	}

	switch parts[0] {
	case "SET":
		if len(parts) >= 3 {
			kv.data[parts[1]] = parts[2]
			return []byte("OK"), nil
		}
	case "GET":
		if len(parts) >= 2 {
			if val, ok := kv.data[parts[1]]; ok {
				return []byte(val), nil
			}
			return []byte("(nil)"), nil
		}
	case "DELETE":
		if len(parts) >= 2 {
			delete(kv.data, parts[1])
			return []byte("OK"), nil
		}
	}

	return nil, nil
}

// Get returns a value from the KV store (for testing/inspection)
func (kv *KVStateMachine) Get(key string) (string, bool) {
	val, ok := kv.data[key]
	return val, ok
}

// splitCommand splits a command string into parts
func splitCommand(cmd string) []string {
	var parts []string
	var current []byte
	inQuote := false

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if c == '"' {
			inQuote = !inQuote
		} else if c == ' ' && !inQuote {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = nil
			}
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}
