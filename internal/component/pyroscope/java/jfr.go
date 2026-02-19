package java

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/alloy/internal/component/pyroscope"
	"github.com/prometheus/prometheus/model/labels"
)

type JFRIngestOptions struct {
	StartTime   time.Time
	EndTime     time.Time
	SampleRate  int64
	SpyName     string
	Units       string // default: "samples"
	Aggregation string // default: "sum"
}

func buildJFRIngestProfile(jfrBytes []byte, ls labels.Labels, opts JFRIngestOptions) (*pyroscope.IncomingProfile, error) {
	if opts.Units == "" {
		opts.Units = "samples"
	}
	if opts.Aggregation == "" {
		opts.Aggregation = "sum"
	}

	// Build URL with query parameters for JFR format
	u := &url.URL{Path: "/ingest"}
	q := u.Query()
	q.Set("format", "jfr")
	q.Set("units", opts.Units)
	q.Set("aggregationType", opts.Aggregation)
	q.Set("sampleRate", strconv.FormatInt(opts.SampleRate, 10))
	q.Set("from", strconv.FormatInt(opts.StartTime.Unix(), 10))
	q.Set("until", strconv.FormatInt(opts.EndTime.Unix(), 10))
	q.Set("spyName", opts.SpyName)
	u.RawQuery = q.Encode()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("jfr", "profile.jfr")
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(jfrBytes); err != nil {
		return nil, fmt.Errorf("failed to write jfr data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &pyroscope.IncomingProfile{
		RawBody:     body.Bytes(),
		ContentType: []string{writer.FormDataContentType()},
		URL:         u,
		Labels:      ls,
	}, nil
}
