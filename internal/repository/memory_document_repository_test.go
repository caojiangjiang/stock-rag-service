package repository

import (
	"context"
	"testing"
)

func TestMemoryDocumentRepositoryList(t *testing.T) {
	repo := NewMemoryDocumentRepository()

	docs, err := repo.ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected 0 documents, got %d", len(docs))
	}
}
