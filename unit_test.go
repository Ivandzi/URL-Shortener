package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type MockResolver struct {
	resolveFunc func(ctx context.Context, code string) (string, error)
}
type MockSaver struct {
	saveFunc func(ctx context.Context, code string, originalURL string) error
}

func (m *MockResolver) ResolveShortCode(ctx context.Context, code string) (string, error) {
	return m.resolveFunc(ctx, code)
}
func (m *MockSaver) SaveURL(ctx context.Context, code string, originalURL string) error {
	return m.saveFunc(ctx, code, originalURL)
}
func TestGenerateShortCode(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code, err := GenerateShortCode()
		if err != nil {
			t.Errorf("Generation error: %v", err)
		}
		if code == "" {
			t.Error("generateShortCode is empty, expected code")
		}
		if len(code) != 8 {
			t.Errorf("Expected length 8, got %v", len(code))
		}
		if seen[code] {
			t.Errorf("Duplicate code generated: %v", code)
		}
		seen[code] = true
	}
}
func TestResolveShortCode(t *testing.T) {
	resolve := &MockResolver{
		resolveFunc: func(ctx context.Context, code string) (string, error) {
			return "https://example.com", nil
		},
	}
	ctx := context.Background()
	result, err := resolve.ResolveShortCode(ctx, "test_code")
	if err != nil {
		t.Errorf("Resolve error: %v", err)
	}
	if result != "https://example.com" {
		t.Errorf("Expected https://example.com, got %v", result)
	}
}
func TestSaveURL(t *testing.T) {
	save := &MockSaver{
		saveFunc: func(ctx context.Context, code string, originalURL string) error {
			return nil
		},
	}
	ctx := context.Background()
	if err := save.SaveURL(ctx, "test_code", "https://example.com"); err != nil {
		t.Errorf("Save error: %v", err)
	}
}
func TestShortenHandler_MethodNotAllowed(t *testing.T) {
	repo := &Repository{}
	req := httptest.NewRequest(http.MethodGet, "/shorten", nil)
	w := httptest.NewRecorder()
	repo.ShortenHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %v", w.Code)
	}
}
func TestShortenHandler_InvalidJSON(t *testing.T) {
	repo := &Repository{}
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	repo.ShortenHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %v", w.Code)
	}
}
func TestResolveHandler_MethodNotAllowed(t *testing.T) {
	handler := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/resolve", nil)
	w := httptest.NewRecorder()
	handler.ResolveHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %v", w.Code)
	}
}
func TestResolveHandler_NotFound(t *testing.T) {
	mock := &MockResolver{
		resolveFunc: func(ctx context.Context, code string) (string, error) {
			return "", pgx.ErrNoRows
		},
	}
	handler := NewHandler(mock)
	mux := http.NewServeMux()
	mux.HandleFunc("/resolve/{code}", handler.ResolveHandler)
	req := httptest.NewRequest(http.MethodGet, "/resolve/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %v", w.Code)
	}
}
