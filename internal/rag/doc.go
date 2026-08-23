// Package rag implements Agentic Retrieval-Augmented Generation.
//
// RAG is exposed as an agent tool, not a fixed pipeline. The agent
// decides when, what, and how to retrieve. Supports semantic chunking,
// hybrid search (vector + BM25), cross-encoder re-ranking, and
// query transformation.
package rag
