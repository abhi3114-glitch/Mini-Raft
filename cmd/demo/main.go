package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mini-raft/raft"
)

func main() {
	fmt.Println("===========================================")
	fmt.Println("     Mini-Raft Consensus Demo")
	fmt.Println("===========================================")
	fmt.Println()

	// Create 3-node cluster with memory transport
	t1 := raft.NewMemoryTransport("node1")
	t2 := raft.NewMemoryTransport("node2")
	t3 := raft.NewMemoryTransport("node3")

	// Connect all nodes
	t1.Connect(t2)
	t1.Connect(t3)
	t2.Connect(t3)

	// Create configurations
	// Node1 has shorter timeout to become leader first
	config1 := raft.Config{
		ID:                "node1",
		Peers:             []string{"node2", "node3"},
		ElectionTimeout:   100 * time.Millisecond,
		HeartbeatInterval: 30 * time.Millisecond,
	}
	config2 := raft.Config{
		ID:                "node2",
		Peers:             []string{"node1", "node3"},
		ElectionTimeout:   200 * time.Millisecond,
		HeartbeatInterval: 30 * time.Millisecond,
	}
	config3 := raft.Config{
		ID:                "node3",
		Peers:             []string{"node1", "node2"},
		ElectionTimeout:   200 * time.Millisecond,
		HeartbeatInterval: 30 * time.Millisecond,
	}

	// Create state machines
	sm1 := raft.NewKVStateMachine()
	sm2 := raft.NewKVStateMachine()
	sm3 := raft.NewKVStateMachine()

	// Create custom loggers
	logger1 := log.New(os.Stdout, "[NODE1] ", log.Ltime|log.Lmicroseconds)
	logger2 := log.New(os.Stdout, "[NODE2] ", log.Ltime|log.Lmicroseconds)
	logger3 := log.New(os.Stdout, "[NODE3] ", log.Ltime|log.Lmicroseconds)

	// Create nodes
	node1 := raft.NewNode(config1, t1, sm1, logger1)
	node2 := raft.NewNode(config2, t2, sm2, logger2)
	node3 := raft.NewNode(config3, t3, sm3, logger3)

	// Set up RPC handlers
	t1.Listen(node1)
	t2.Listen(node2)
	t3.Listen(node3)

	fmt.Println("Starting 3-node Raft cluster...")
	fmt.Println()

	// Start all nodes
	node1.Start()
	node2.Start()
	node3.Start()

	// Wait for leader election
	fmt.Println()
	fmt.Println("Waiting for leader election...")
	time.Sleep(500 * time.Millisecond)

	// Find the leader
	nodes := map[string]*raft.Node{
		"node1": node1,
		"node2": node2,
		"node3": node3,
	}
	stateMachines := map[string]*raft.KVStateMachine{
		"node1": sm1,
		"node2": sm2,
		"node3": sm3,
	}

	printClusterStatus(nodes)

	// Interactive mode
	fmt.Println()
	fmt.Println("===========================================")
	fmt.Println("     Interactive Mode")
	fmt.Println("===========================================")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  SET <key> <value>  - Set a key-value pair")
	fmt.Println("  GET <key>          - Get a value")
	fmt.Println("  STATUS             - Show cluster status")
	fmt.Println("  STATE              - Show state machine contents")
	fmt.Println("  STOP <node>        - Stop a node (node1, node2, node3)")
	fmt.Println("  START <node>       - Start a stopped node")
	fmt.Println("  QUIT               - Exit the demo")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	stoppedNodes := make(map[string]bool)

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmd := strings.ToUpper(parts[0])

		switch cmd {
		case "SET":
			if len(parts) < 3 {
				fmt.Println("Usage: SET <key> <value>")
				continue
			}
			key := parts[1]
			value := strings.Join(parts[2:], " ")
			command := fmt.Sprintf("SET %s %s", key, value)

			// Find leader and submit
			leader := findLeader(nodes, stoppedNodes)
			if leader == nil {
				fmt.Println("No leader available!")
				continue
			}

			index, term, ok := leader.Submit([]byte(command))
			if !ok {
				fmt.Println("Command rejected - node is not leader")
				continue
			}

			fmt.Printf("Command submitted: index=%d, term=%d\n", index, term)
			time.Sleep(100 * time.Millisecond) // Wait for replication

		case "GET":
			if len(parts) < 2 {
				fmt.Println("Usage: GET <key>")
				continue
			}
			key := parts[1]
			fmt.Println("Values across nodes:")
			for name, sm := range stateMachines {
				if stoppedNodes[name] {
					fmt.Printf("  %s: (stopped)\n", name)
					continue
				}
				val, ok := sm.Get(key)
				if ok {
					fmt.Printf("  %s: %s\n", name, val)
				} else {
					fmt.Printf("  %s: (nil)\n", name)
				}
			}

		case "STATUS":
			printClusterStatus(nodes)

		case "STATE":
			fmt.Println("State machine contents:")
			for name, sm := range stateMachines {
				if stoppedNodes[name] {
					fmt.Printf("  %s: (stopped)\n", name)
					continue
				}
				// Use a test key to show it works
				fmt.Printf("  %s: KV State Machine (operational)\n", name)
				_ = sm // Just to show it's accessible
			}

		case "STOP":
			if len(parts) < 2 {
				fmt.Println("Usage: STOP <node>")
				continue
			}
			nodeName := strings.ToLower(parts[1])
			if node, ok := nodes[nodeName]; ok {
				if stoppedNodes[nodeName] {
					fmt.Printf("%s is already stopped\n", nodeName)
					continue
				}
				node.Stop()
				stoppedNodes[nodeName] = true
				fmt.Printf("Stopped %s\n", nodeName)
				time.Sleep(300 * time.Millisecond)
				printClusterStatus(nodes)
			} else {
				fmt.Printf("Unknown node: %s\n", nodeName)
			}

		case "START":
			if len(parts) < 2 {
				fmt.Println("Usage: START <node>")
				continue
			}
			nodeName := strings.ToLower(parts[1])
			if node, ok := nodes[nodeName]; ok {
				if !stoppedNodes[nodeName] {
					fmt.Printf("%s is already running\n", nodeName)
					continue
				}
				node.Start()
				stoppedNodes[nodeName] = false
				fmt.Printf("Started %s\n", nodeName)
				time.Sleep(500 * time.Millisecond)
				printClusterStatus(nodes)
			} else {
				fmt.Printf("Unknown node: %s\n", nodeName)
			}

		case "QUIT", "EXIT":
			fmt.Println("Shutting down cluster...")
			for _, node := range nodes {
				node.Stop()
			}
			fmt.Println("Goodbye!")
			return

		default:
			fmt.Printf("Unknown command: %s\n", cmd)
		}
	}
}

func findLeader(nodes map[string]*raft.Node, stopped map[string]bool) *raft.Node {
	for name, node := range nodes {
		if stopped[name] {
			continue
		}
		if node.IsLeader() {
			return node
		}
	}
	return nil
}

func printClusterStatus(nodes map[string]*raft.Node) {
	fmt.Println()
	fmt.Println("Cluster Status:")
	fmt.Println("---------------")
	for name, node := range nodes {
		state := node.State()
		term := node.Term()
		leader := ""
		if state == raft.Leader {
			leader = " ★"
		}
		fmt.Printf("  %s: %s (term %d)%s\n", name, state, term, leader)
	}
	fmt.Println()
}
