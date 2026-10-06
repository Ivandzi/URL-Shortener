package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "urlshortener.com/docs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	httpSwagger "github.com/swaggo/http-swagger"
)

type Repository struct {
	pool        *pgxpool.Pool
	redisClient *redis.Client
}
type Handler struct {
	resolver URLResolver
}
type ShortenRequest struct {
	URL string `json:"url"`
}

func NewRepository(pool *pgxpool.Pool, redisClient *redis.Client) *Repository {
	return &Repository{
		pool:        pool,
		redisClient: redisClient,
	}
}
func NewHandler(resolver URLResolver) *Handler {
	return &Handler{resolver: resolver}
}
func (r *Repository) SaveURL(ctx context.Context, code string, originalURL string) error {
	_, err := r.pool.Exec(ctx,
		"INSERT INTO urls (code,original_url) VALUES ($1, $2)",
		code, originalURL,
	)
	return err
}
func GenerateShortCode() (string, error) {
	code := make([]byte, 6)
	_, err := rand.Read(code)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(code), nil
}
func (r *Repository) ResolveShortCode(ctx context.Context, code string) (string, error) {
	val, err := r.redisClient.Get(ctx, code).Result()
	if err == nil {
		return val, nil
	}
	if err != redis.Nil {
		log.Printf("Redis error: %v", err)
		return "", err
	}
	var originalURL string
	err = r.pool.QueryRow(ctx,
		"SELECT original_url FROM urls WHERE code = $1",
		code,
	).Scan(&originalURL)
	if err != nil {
		return "", err
	}
	err = r.redisClient.Set(ctx, code, originalURL, 10*time.Minute).Err()
	if err != nil {
		log.Printf("Redis set error: %v", err)
	}
	return originalURL, err
}

// ShortenHandler creates a short link
// @Summary create a short link
// @Description It takes the original URL and returns a short code
// @Tags urls
// @Accept json
// @Produce json
// @Param request body ShortenRequest true "Original URL"
// @Success 200 {string} string "Short code"
// @Failure 400 {string} string "Invalid JSON or empty URL"
// @Failure 500 {string} string "Database error"
// @Router /shorten [post]
func (r *Repository) ShortenHandler(w http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code, err := GenerateShortCode()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		log.Printf("Generation error: %v", err)
		return
	}
	var req ShortenRequest
	err = json.NewDecoder(httpReq.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		log.Printf("Decoding error: %v", err)
		return
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if err := r.SaveURL(httpReq.Context(), code, req.URL); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		log.Printf("Save error: %v", err)
		return
	}
	fmt.Fprintln(w, code)
}

// ResolveHandler redirects via a short code
// @Summary redirect using a short code
// @Description It accepts a short code and redirects to the original URL
// @Tags urls
// @Param code path string true "Short code"
// @Success 302 {string} string "Redirect"
// @Failure 400 {string} string "Empty code"
// @Failure 404 {string} string "Code not found"
// @Failure 500 {string} string "Internal error"
// @Router /resolve/{code} [get]
func (h *Handler) ResolveHandler(w http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := httpReq.PathValue("code")
	if code == "" {
		http.Error(w, "code is empty", http.StatusBadRequest)
		return
	}
	originalURL, err := h.resolver.ResolveShortCode(httpReq.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		log.Printf("Resolve error: %v", err)
		return
	}
	http.Redirect(w, httpReq, originalURL, http.StatusFound)
}

type URLSaver interface {
	SaveURL(ctx context.Context, code string, originalURL string) error
}
type URLResolver interface {
	ResolveShortCode(ctx context.Context, code string) (string, error)
}

// @title URL Shortener API
// @version 1.0
// @description A link shortening service with caching in Redis
// @host Localhost:8080
// @BasePath /
func main() {
	hostFlag := flag.String("host", "0.0.0.0", "host name")
	portFlag := flag.Int("port", 8080, "port number")
	flag.Parse()
	host := *hostFlag
	httpPort := *portFlag
	_ = godotenv.Load()
	user := os.Getenv("DATABASE_LOGIN")
	if user == "" {
		log.Fatal("DATABASE_LOGIN is not set")
	}
	password := os.Getenv("DATABASE_PASSWORD")
	if password == "" {
		log.Fatal("DATABASE_PASSWORD is not set")
	}
	dbHost := os.Getenv("DATABASE_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("DATABASE_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbName := os.Getenv("DATABASE_NAME")
	if dbName == "" {
		log.Fatal("DATABASE_NAME is not set")
	}
	connString := fmt.Sprintf("postgres://%v:%v@%v:%v/%v?sslmode=disable", user, password, dbHost, dbPort, dbName)
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}
	defer pool.Close()
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}
	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	client := redis.NewClient(&redis.Options{
		Addr:     redisHost + ":" + redisPort,
		Password: redisPassword,
		DB:       0,
	})
	defer client.Close()
	_, err = client.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Error connecting to redis: %v", err)
	}
	repo := NewRepository(pool, client)
	handler := NewHandler(repo)
	http.HandleFunc("/shorten", repo.ShortenHandler)
	http.HandleFunc("/resolve/{code}", handler.ResolveHandler)
	http.HandleFunc("/swagger/", httpSwagger.WrapHandler)
	addr := fmt.Sprintf("%v:%v", host, httpPort)
	log.Printf("Server is running on %v", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
