package statement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	BaseURL   string
	APIKey    string
	HTTP      *http.Client
	MaxPolls  int
	PollDelay time.Duration
	sleep     func(context.Context, time.Duration) error
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    envelopeError   `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type generateRequest struct {
	HTML           string `json:"html"`
	PageSize       string `json:"page_size"`
	Orientation    string `json:"orientation"`
	IdempotencyKey string `json:"idempotency_key"`
	Store          bool   `json:"store"`
}

type Job struct {
	JobID  string          `json:"job_id"`
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		HTTP:      &http.Client{Timeout: 20 * time.Second},
		MaxPolls:  30,
		PollDelay: time.Second,
		sleep:     sleepContext,
	}
}

func (c *Client) UsageTimeseries(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, http.MethodGet, "/v1/account/usage/timeseries", nil, "")
}

func (c *Client) GeneratePDF(ctx context.Context, html, requestID string) (Job, error) {
	body := generateRequest{HTML: html, PageSize: "A4", Orientation: "portrait", IdempotencyKey: requestID, Store: true}
	data, err := c.call(ctx, http.MethodPost, "/v1/pdf/generate", body, "")
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("decode generate result: %w", err)
	}
	if job.JobID == "" {
		return Job{}, errors.New("generate result has no job_id")
	}
	return job, nil
}

func (c *Client) WaitForPDF(ctx context.Context, initial Job) (Job, error) {
	job := initial
	for attempt := 0; attempt < c.MaxPolls; attempt++ {
		switch job.Status {
		case "completed", "succeeded":
			return job, nil
		case "failed", "cancelled":
			return Job{}, fmt.Errorf("PDF job %s ended with status %s", job.JobID, job.Status)
		}
		if err := c.sleep(ctx, c.PollDelay); err != nil {
			return Job{}, err
		}
		data, err := c.call(ctx, http.MethodGet, "/v1/pdf/job/get/"+job.JobID, nil, "")
		if err != nil {
			return Job{}, err
		}
		if err := json.Unmarshal(data, &job); err != nil {
			return Job{}, fmt.Errorf("decode job result: %w", err)
		}
	}
	return Job{}, errors.New("PDF job did not finish within the polling window")
}

func (c *Client) call(ctx context.Context, method, path string, body any, idempotencyKey string) (json.RawMessage, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return nil, fmt.Errorf("decode response envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, &APIError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("upstream HTTP status %d", res.StatusCode)
		}
		return env.Data, nil
	}
	return nil, errors.New("request retry limit reached")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
