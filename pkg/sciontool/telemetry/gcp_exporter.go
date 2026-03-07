/*
Copyright 2025 The Scion Authors.
*/

package telemetry

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/logging"
	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel/sdk/trace"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/api/option"

	scionlog "github.com/ptone/scion-agent/pkg/sciontool/log"
)

// GCPExporter exports telemetry data to GCP using native APIs.
// It uses Cloud Trace for spans and Cloud Logging for logs.
// Metrics are forwarded via the SDK metric exporter (see providers.go).
type GCPExporter struct {
	traceExporter trace.SpanExporter
	logClient     *logging.Client
	logger        *logging.Logger
	projectID     string
}

// NewGCPExporter creates a new GCP-native exporter for traces and logs.
func NewGCPExporter(config *Config) (*GCPExporter, error) {
	ctx := context.Background()

	if config.ProjectID == "" {
		return nil, fmt.Errorf("GCP project ID is required (set SCION_GCP_PROJECT_ID or provide credentials file with project_id)")
	}

	opts := []option.ClientOption{}
	if config.GCPCredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(config.GCPCredentialsFile))
	}

	// Create GCP Cloud Trace exporter
	traceOpts := []texporter.Option{
		texporter.WithProjectID(config.ProjectID),
	}
	if len(opts) > 0 {
		traceOpts = append(traceOpts, texporter.WithTraceClientOptions(opts))
	}
	traceExp, err := texporter.New(traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating GCP trace exporter: %w", err)
	}

	// Create Cloud Logging client for log forwarding
	logClient, err := logging.NewClient(ctx, config.ProjectID, opts...)
	if err != nil {
		_ = traceExp.Shutdown(ctx)
		return nil, fmt.Errorf("creating Cloud Logging client: %w", err)
	}

	// Build common labels for agent identification
	commonLabels := map[string]string{}
	if agentID := os.Getenv("SCION_AGENT_ID"); agentID != "" {
		commonLabels["agent_id"] = agentID
	}
	if groveID := os.Getenv("SCION_GROVE_ID"); groveID != "" {
		commonLabels["grove_id"] = groveID
	}

	var loggerOpts []logging.LoggerOption
	if len(commonLabels) > 0 {
		loggerOpts = append(loggerOpts, logging.CommonLabels(commonLabels))
	}

	return &GCPExporter{
		traceExporter: traceExp,
		logClient:     logClient,
		logger:        logClient.Logger("scion-agents", loggerOpts...),
		projectID:     config.ProjectID,
	}, nil
}

// ExportProtoSpans converts OTLP proto spans to SDK ReadOnlySpan and exports
// via the GCP Cloud Trace exporter.
func (e *GCPExporter) ExportProtoSpans(ctx context.Context, resourceSpans []*tracepb.ResourceSpans) error {
	if e == nil || e.traceExporter == nil {
		return nil
	}

	sdkSpans := protoResourceSpansToSDK(resourceSpans)
	if len(sdkSpans) == 0 {
		return nil
	}

	return e.traceExporter.ExportSpans(ctx, sdkSpans)
}

// ExportProtoMetrics is a no-op for the GCP exporter. Metrics are handled
// by the SDK MeterProvider configured in providers.go with the GCP metric
// exporter. Pipeline-received metrics from agents are not forwarded via this
// path since the GCP metric exporter requires SDK metricdata types.
func (e *GCPExporter) ExportProtoMetrics(ctx context.Context, resourceMetrics []*metricpb.ResourceMetrics) error {
	if e == nil {
		return nil
	}
	// Log that metrics arrived but can't be forwarded in GCP mode.
	// The agent's own metric providers handle their own export.
	dpCount := 0
	for _, rm := range resourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			dpCount += len(sm.Metrics)
		}
	}
	if dpCount > 0 {
		scionlog.Debug("GCP exporter: received %d metrics (pipeline metric forwarding not supported in GCP-native mode)", dpCount)
	}
	return nil
}

// ExportProtoLogs converts OTLP proto log records to Cloud Logging entries.
func (e *GCPExporter) ExportProtoLogs(ctx context.Context, resourceLogs []*logspb.ResourceLogs) error {
	if e == nil || e.logger == nil {
		return nil
	}

	for _, rl := range resourceLogs {
		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				entry := protoLogToCloudEntry(lr, rl.Resource)
				e.logger.Log(entry)
			}
		}
	}

	return nil
}

// Shutdown flushes and closes all GCP clients.
func (e *GCPExporter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}

	var errs []error

	if e.traceExporter != nil {
		if err := e.traceExporter.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("trace exporter shutdown: %w", err))
		}
	}

	if e.logClient != nil {
		if err := e.logClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("log client close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
