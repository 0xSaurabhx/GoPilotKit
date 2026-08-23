// Package runtime implements the graph-based agent execution engine.
//
// The runtime is a DAG-based execution engine with parallel branches,
// conditional paths, checkpoint/resume, and human-in-the-loop as a
// first-class primitive. Agents, tools, and functions are all nodes
// in the same graph.
package runtime
