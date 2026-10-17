package statement

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExportPipeline(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus string
		pollStatuses  []string
		wantPolls     int
	}{
		{name: "queued work is polled to completion", initialStatus: "queued", pollStatuses: []string{"processing", "completed"}, wantPolls: 2},
		{name: "completed work is delivered immediately", initialStatus: "completed", wantPolls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/account/usage/timeseries":
					io.WriteString(w, `{"ok":true,"data":{"series":[{"meter":"stream_minutes","value":840}]},"metadata":{}}`)
				case r.Method == http.MethodPost && r.URL.Path == "/v1/pdf/generate":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["idempotency_key"] != "export-2026-08" {
						t.Errorf("idempotency key = %q", body["idempotency_key"])
					}
					if !strings.Contains(body["html"].(string), "stream_minutes") {
						t.Error("generated HTML does not include usage")
					}
					io.WriteString(w, `{"ok":true,"data":{"job_id":"job-42","status":"`+tt.initialStatus+`"},"metadata":{}}`)
				case r.Method == http.MethodGet && r.URL.Path == "/v1/pdf/job/get/job-42":
					mu.Lock()
					status := tt.pollStatuses[polls]
					polls++
					mu.Unlock()
					result := "null"
					if status == "completed" {
						result = `{"download_url":"https://media.example/statements/job-42.pdf"}`
					}
					io.WriteString(w, `{"ok":true,"data":{"job_id":"job-42","status":"`+status+`","result":`+result+`},"metadata":{}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-key")
			client.MaxPolls = 4
			client.PollDelay = 0
			client.sleep = func(context.Context, time.Duration) error { return nil }
			pipeline := Pipeline{Client: client, Now: func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }}
			delivery, err := pipeline.Export(context.Background(), ExportRequest{
				RequestID: "export-2026-08", CreatorName: "North Room Media", PeriodLabel: "August 2026",
			})
			if err != nil {
				t.Fatal(err)
			}
			if delivery.Status != "ready" || delivery.JobID != "job-42" {
				t.Fatalf("delivery = %+v", delivery)
			}
			if polls != tt.wantPolls {
				t.Fatalf("poll count = %d, want %d", polls, tt.wantPolls)
			}
		})
	}
}

func TestGeneratePDFRetriesRateLimitWithSameIdempotencyKey(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/pdf/generate" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["idempotency_key"] != "stable-export-id" {
			t.Fatalf("idempotency key = %q", body["idempotency_key"])
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"ok":false,"data":null,"error":{"message":"retry later"},"metadata":{}}`)
			return
		}
		io.WriteString(w, `{"ok":true,"data":{"job_id":"job-9","status":"queued"},"metadata":{}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	client.sleep = func(context.Context, time.Duration) error { return nil }
	job, err := client.GeneratePDF(context.Background(), "<p>statement</p>", "stable-export-id")
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != "job-9" || requests != 2 {
		t.Fatalf("job = %+v, requests = %d", job, requests)
	}
}
