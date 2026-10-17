package statement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"
)

type ExportRequest struct {
	RequestID   string `json:"request_id"`
	CreatorName string `json:"creator_name"`
	PeriodLabel string `json:"period_label"`
}

type Delivery struct {
	RequestID string          `json:"request_id"`
	JobID     string          `json:"job_id"`
	Status    string          `json:"status"`
	PDF       json.RawMessage `json:"pdf"`
}

type Pipeline struct {
	Client *Client
	Now    func() time.Time
}

var statementTemplate = template.Must(template.New("statement").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Media account statement</title></head>
<body><h1>Media account statement</h1>
<p>Creator: {{.CreatorName}}</p><p>Period: {{.PeriodLabel}}</p><p>Exported: {{.ExportedAt}}</p>
<h2>Metered usage</h2><pre>{{.Usage}}</pre></body></html>`))

func (p *Pipeline) Export(ctx context.Context, input ExportRequest) (Delivery, error) {
	if strings.TrimSpace(input.RequestID) == "" || strings.TrimSpace(input.CreatorName) == "" || strings.TrimSpace(input.PeriodLabel) == "" {
		return Delivery{}, &APIError{Code: "INVALID_EXPORT_REQUEST", Message: "request_id, creator_name, and period_label are required", HTTPStatus: 400}
	}
	usage, err := p.Client.UsageTimeseries(ctx)
	if err != nil {
		return Delivery{}, err
	}
	html, err := renderStatement(input, usage, p.Now().UTC())
	if err != nil {
		return Delivery{}, err
	}
	job, err := p.Client.GeneratePDF(ctx, html, input.RequestID)
	if err != nil {
		return Delivery{}, err
	}
	job, err = p.Client.WaitForPDF(ctx, job)
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{RequestID: input.RequestID, JobID: job.JobID, Status: "ready", PDF: job.Result}, nil
}

func renderStatement(input ExportRequest, usage json.RawMessage, exportedAt time.Time) (string, error) {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, usage, "", "  "); err != nil {
		return "", fmt.Errorf("format usage data: %w", err)
	}
	var output strings.Builder
	err := statementTemplate.Execute(&output, struct {
		CreatorName string
		PeriodLabel string
		ExportedAt  string
		Usage       string
	}{input.CreatorName, input.PeriodLabel, exportedAt.Format(time.RFC3339), pretty.String()})
	if err != nil {
		return "", fmt.Errorf("render statement: %w", err)
	}
	return output.String(), nil
}
