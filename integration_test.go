package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestSaveAndResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@localhost:5432/urls?sslmode=require")
	if err != nil {
		t.Fatalf("Failed to connect to db: %v", err)
	}
	defer pool.Close()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	repo := NewRepository(pool, client)
	_, _ = pool.Exec(context.Background(), "DELETE FROM urls WHERE code = $1", "test_integration")
	_ = client.Del(context.Background(), "test_integration")
	err = repo.SaveURL(context.Background(), "test_integration", "http://example.com")
	if err != nil {
		t.Fatalf("SaveURL failed: %v", err)
	}
	result, err := repo.ResolveShortCode(context.Background(), "test_integration")
	if err != nil {
		t.Fatalf("ResolveShortCode failed: %v", err)
	}
	if result != "http://example.com" {
		t.Errorf("Expected http://example.com, got %v", result)
	}
	cached, err := client.Get(context.Background(), "test_integration").Result()
	if err != nil {
		t.Fatalf("Cache miss: %v", err)
	}
	if cached != "http://example.com" {
		t.Errorf("Expected cached http://example.com, got %v", cached)
	}
}
