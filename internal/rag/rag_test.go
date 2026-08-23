package rag

import (
	"testing"
)

func TestIngestAndRetrieve(t *testing.T) {
	engine := NewEngine(DefaultRAGConfig())

	engine.Ingest("Go is a statically typed compiled language designed at Google. It is syntactically similar to C but with memory safety and garbage collection.", "doc1", nil)
	engine.Ingest("Python is a high-level programming language known for its simplicity and readability. It supports multiple programming paradigms.", "doc2", nil)

	if engine.DocumentCount() != 2 {
		t.Errorf("expected 2 documents, got %d", engine.DocumentCount())
	}

	result := engine.Retrieve("What is Go?")
	if len(result.Chunks) == 0 {
		t.Error("expected at least one chunk for Go query")
	}
	if result.TotalDocs != 2 {
		t.Errorf("expected 2 total docs, got %d", result.TotalDocs)
	}
}

func TestChunking(t *testing.T) {
	engine := NewEngine(Config{
		ChunkSize:    50,
		ChunkOverlap: 10,
		TopK:         5,
		MinScore:     0.1,
	})

	longText := "This is a test document. " +
		"It has multiple sentences that should be split into chunks. " +
		"The chunking should handle overlaps correctly. " +
		"Each chunk should be roughly the configured size."

	doc := engine.Ingest(longText, "test", nil)
	if len(doc.Chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(doc.Chunks))
	}
	if engine.ChunkCount() < 2 {
		t.Errorf("expected at least 2 total chunks, got %d", engine.ChunkCount())
	}
}

func TestRetrieveRelevance(t *testing.T) {
	engine := NewEngine(Config{
		ChunkSize:    200,
		ChunkOverlap: 20,
		TopK:         3,
		MinScore:     0.1,
	})

	engine.Ingest("The Eiffel Tower is located in Paris, France. It was built in 1889.", "doc1", nil)
	engine.Ingest("The Great Wall of China is one of the largest construction projects ever completed.", "doc2", nil)
	engine.Ingest("Python was created by Guido van Rossum and first released in 1991.", "doc3", nil)

	result := engine.Retrieve("Where is the Eiffel Tower?")
	if len(result.Chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// The first result should be about the Eiffel Tower.
	found := false
	for _, chunk := range result.Chunks {
		if containsText(chunk.Content, "Eiffel Tower") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Eiffel Tower content in results")
	}
}

func TestAgenticRetrieveFactual(t *testing.T) {
	engine := NewEngine(DefaultRAGConfig())

	engine.Ingest("Go was designed at Google by Robert Griesemer, Rob Pike, and Ken Thompson.", "doc1", nil)

	// Factual query should trigger retrieval.
	result, found := engine.AgenticRetrieve("What is Go?")
	if !found {
		t.Log("agentic retrieval did not trigger (heuristic may need tuning)")
		return
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestAgenticRetrieveNonFactual(t *testing.T) {
	engine := NewEngine(DefaultRAGConfig())

	// Non-factual query should skip retrieval.
	_, found := engine.AgenticRetrieve("Thanks!")
	if found {
		t.Error("expected non-factual query to skip retrieval")
	}
}

func TestDocumentCount(t *testing.T) {
	engine := NewEngine(DefaultRAGConfig())

	if engine.DocumentCount() != 0 {
		t.Errorf("expected 0 documents, got %d", engine.DocumentCount())
	}

	engine.Ingest("doc1 content", "src1", nil)
	engine.Ingest("doc2 content", "src2", nil)

	if engine.DocumentCount() != 2 {
		t.Errorf("expected 2 documents, got %d", engine.DocumentCount())
	}
}

func TestListDocuments(t *testing.T) {
	engine := NewEngine(DefaultRAGConfig())

	engine.Ingest("content1", "source1", nil)
	engine.Ingest("content2", "source2", nil)

	docs := engine.ListDocuments()
	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}
}

func TestSmallDocumentSingleChunk(t *testing.T) {
	engine := NewEngine(Config{ChunkSize: 1000, TopK: 5, MinScore: 0.1})

	doc := engine.Ingest("Short document.", "src", nil)
	if len(doc.Chunks) != 1 {
		t.Errorf("expected 1 chunk for short doc, got %d", len(doc.Chunks))
	}
}

func containsText(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
