package rag

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEmbeddedStoreInsertAndSearch(t *testing.T) {
	store := NewEmbeddedStore()
	defer store.Close()

	ctx := context.Background()

	// Insert some chunks with TF-IDF embeddings.
	chunks := []Chunk{
		{ID: "c1", DocID: "d1", Content: "machine learning algorithms for classification", Position: 0, Source: "doc1.txt", TokenCount: 7, Embedding: textToVector("machine learning algorithms for classification")},
		{ID: "c2", DocID: "d1", Content: "deep neural networks and transformers", Position: 1, Source: "doc1.txt", TokenCount: 5, Embedding: textToVector("deep neural networks and transformers")},
		{ID: "c3", DocID: "d2", Content: "natural language processing with transformers", Position: 0, Source: "doc2.txt", TokenCount: 6, Embedding: textToVector("natural language processing with transformers")},
	}

	for _, c := range chunks {
		if err := store.Insert(ctx, c); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// Search for machine learning.
	results, err := store.Search(ctx, textToVector("machine learning"), 3, 0.1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// The first result should be most related to machine learning.
	if results[0].Chunk.ID != "c1" {
		t.Errorf("expected top result to be c1 (machine learning), got %s", results[0].Chunk.ID)
	}
}

func TestEmbeddedStoreDeleteByDocID(t *testing.T) {
	store := NewEmbeddedStore()
	defer store.Close()

	ctx := context.Background()

	store.Insert(ctx, Chunk{ID: "c1", DocID: "d1", Content: "test", Embedding: textToVector("test")})
	store.Insert(ctx, Chunk{ID: "c2", DocID: "d1", Content: "test2", Embedding: textToVector("test2")})
	store.Insert(ctx, Chunk{ID: "c3", DocID: "d2", Content: "other", Embedding: textToVector("other")})

	count, _ := store.Count(ctx)
	if count != 3 {
		t.Fatalf("expected 3 chunks, got %d", count)
	}

	store.DeleteByDocID(ctx, "d1")

	count, _ = store.Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1 chunk after delete, got %d", count)
	}
}

func TestEmbeddedStoreCount(t *testing.T) {
	store := NewEmbeddedStore()
	defer store.Close()

	ctx := context.Background()

	count, _ := store.Count(ctx)
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	store.Insert(ctx, Chunk{ID: "c1", DocID: "d1", Embedding: textToVector("test")})

	count, _ = store.Count(ctx)
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}

func TestDocumentStoreIngestAndRetrieve(t *testing.T) {
	store := NewEmbeddedStore()
	defer store.Close()

	ds := NewDocumentStore(store)
	ctx := context.Background()

	// Ingest a document.
	doc, err := ds.Ingest(ctx,
		"Machine learning is a subset of artificial intelligence that focuses on building systems that learn from data. Deep learning uses neural networks with many layers.",
		"ml_intro.txt",
		json.RawMessage(`{"topic": "ML"}`),
		100, 20,
	)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if doc.ID == "" {
		t.Fatal("expected non-empty document ID")
	}

	// Check chunks were stored.
	chunkCount, _ := ds.ChunkCount(ctx)
	if chunkCount < 1 {
		t.Fatalf("expected at least 1 chunk, got %d", chunkCount)
	}

	// Retrieve with a relevant query.
	result, err := ds.Retrieve(ctx, "what is machine learning", 5, 0.1)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(result.Chunks) == 0 {
		t.Fatal("expected at least one result")
	}

	if result.TotalDocs != 1 {
		t.Errorf("expected 1 document, got %d", result.TotalDocs)
	}
}

func TestDocumentStoreMultipleIngests(t *testing.T) {
	store := NewEmbeddedStore()
	defer store.Close()

	ds := NewDocumentStore(store)
	ctx := context.Background()

	ds.Ingest(ctx, "The quick brown fox jumps over the lazy dog.", "doc1.txt", nil, 50, 10)
	ds.Ingest(ctx, "Python is a popular programming language for data science.", "doc2.txt", nil, 50, 10)

	if ds.DocumentCount() != 2 {
		t.Errorf("expected 2 documents, got %d", ds.DocumentCount())
	}

	chunkCount, _ := ds.ChunkCount(ctx)
	if chunkCount < 2 {
		t.Errorf("expected at least 2 chunks, got %d", chunkCount)
	}
}
