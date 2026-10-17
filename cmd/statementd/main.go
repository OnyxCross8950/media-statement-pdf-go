package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"example.com/media-statement-export/internal/statement"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	baseURL := os.Getenv("INFRAI_BASE_URL")
	if baseURL == "" {
		baseURL = statement.DefaultBaseURL
	}
	client := statement.NewClient(baseURL, apiKey)
	handler := statement.Handler{Pipeline: &statement.Pipeline{Client: client, Now: time.Now}}
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("statement service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
