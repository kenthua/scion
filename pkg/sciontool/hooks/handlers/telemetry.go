/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/ptone/scion-agent/pkg/sciontool/log"
	"github.com/ptone/scion-agent/pkg/sciontool/telemetry"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SpanMapping defines how hook events map to span names.
var SpanMapping = map[string]string{
	hooks.EventSessionStart:    "agent.session.start",
	hooks.EventSessionEnd:      "agent.session.end",
	hooks.EventToolStart:       "agent.tool.call",
	hooks.EventToolEnd:         "agent.tool.result",
	hooks.EventPromptSubmit:    "agent.user.prompt",
	hooks.EventModelStart:      "gen_ai.api.request",
	hooks.EventModelEnd:        "gen_ai.api.response",
	hooks.EventAgentStart:      "agent.turn.start",
	hooks.EventAgentEnd:        "agent.turn.end",
	hooks.EventNotification:    "agent.notification",
	hooks.EventResponseComplete: "agent.response.complete",
	hooks.EventPreStart:        "agent.lifecycle.pre_start",
	hooks.EventPostStart:       "agent.lifecycle.post_start",
	hooks.EventPreStop:         "agent.lifecycle.pre_stop",
}

// inProgressSpan tracks a span that has been started but not ended.
type inProgressSpan struct {
	span      trace.Span
	ctx       context.Context
	startTime time.Time
	toolName  string
}

// TelemetryHandler converts hook events to OTLP spans, emits correlated log records,
// and records OTel metrics for token usage, tool calls, and API performance.
type TelemetryHandler struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	redactor   *telemetry.Redactor
	spanStore sync.Map // map[string]*inProgressSpan - keyed by spanKey

	// Metric instruments
	tokensInput    metric.Int64Counter
	tokensOutput   metric.Int64Counter
	tokensCached   metric.Int64Counter
	toolCalls      metric.Int64Counter
	toolDuration   metric.Float64Histogram
	sessionCount   metric.Int64Counter
	apiCalls       metric.Int64Counter
	apiDuration    metric.Float64Histogram
}

// NewTelemetryHandler creates a new telemetry handler.
// If tp is nil, a noop tracer will be used.
// If lp is non-nil, correlated log records will be emitted alongside spans.
// If mp is non-nil, OTel metric instruments will be created for recording counters and histograms.
func NewTelemetryHandler(tp trace.TracerProvider, lp otellog.LoggerProvider, redactor *telemetry.Redactor, mp ...metric.MeterProvider) *TelemetryHandler {
	var tracer trace.Tracer
	if tp != nil {
		tracer = tp.Tracer("github.com/ptone/scion-agent/pkg/sciontool/hooks/handlers")
	} else {
		tracer = trace.NewNoopTracerProvider().Tracer("noop")
	}

	h := &TelemetryHandler{
		tracer:   tracer,
		redactor: redactor,
	}

	if lp != nil {
		h.logger = slog.New(otelslog.NewHandler("sciontool.hooks",
			otelslog.WithLoggerProvider(lp),
		))
	}

	// Initialize metric instruments if a MeterProvider is given
	if len(mp) > 0 && mp[0] != nil {
		h.initMetrics(mp[0])
	}

	return h
}

// initMetrics creates OTel metric instruments on the handler.
func (h *TelemetryHandler) initMetrics(mp metric.MeterProvider) {
	meter := mp.Meter("github.com/ptone/scion-agent/pkg/sciontool/hooks/handlers")

	var err error

	h.tokensInput, err = meter.Int64Counter("gen_ai.tokens.input",
		metric.WithUnit("{token}"),
		metric.WithDescription("Number of input tokens consumed"),
	)
	if err != nil {
		log.Error("Failed to create gen_ai.tokens.input counter: %v", err)
	}

	h.tokensOutput, err = meter.Int64Counter("gen_ai.tokens.output",
		metric.WithUnit("{token}"),
		metric.WithDescription("Number of output tokens generated"),
	)
	if err != nil {
		log.Error("Failed to create gen_ai.tokens.output counter: %v", err)
	}

	h.tokensCached, err = meter.Int64Counter("gen_ai.tokens.cached",
		metric.WithUnit("{token}"),
		metric.WithDescription("Number of tokens served from cache"),
	)
	if err != nil {
		log.Error("Failed to create gen_ai.tokens.cached counter: %v", err)
	}

	h.toolCalls, err = meter.Int64Counter("agent.tool.calls",
		metric.WithUnit("{call}"),
		metric.WithDescription("Number of tool invocations"),
	)
	if err != nil {
		log.Error("Failed to create agent.tool.calls counter: %v", err)
	}

	h.toolDuration, err = meter.Float64Histogram("agent.tool.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Duration of tool invocations"),
	)
	if err != nil {
		log.Error("Failed to create agent.tool.duration histogram: %v", err)
	}

	h.sessionCount, err = meter.Int64Counter("agent.session.count",
		metric.WithUnit("{session}"),
		metric.WithDescription("Number of agent sessions"),
	)
	if err != nil {
		log.Error("Failed to create agent.session.count counter: %v", err)
	}

	h.apiCalls, err = meter.Int64Counter("gen_ai.api.calls",
		metric.WithUnit("{call}"),
		metric.WithDescription("Number of LLM API calls"),
	)
	if err != nil {
		log.Error("Failed to create gen_ai.api.calls counter: %v", err)
	}

	h.apiDuration, err = meter.Float64Histogram("gen_ai.api.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Duration of LLM API calls"),
	)
	if err != nil {
		log.Error("Failed to create gen_ai.api.duration histogram: %v", err)
	}
}

// Handle processes a hook event and emits a corresponding span.
func (h *TelemetryHandler) Handle(event *hooks.Event) error {
	if h == nil || event == nil {
		return nil
	}

	spanName, ok := SpanMapping[event.Name]
	if !ok {
		// Unknown event type - skip
		return nil
	}

	// Handle start/end pairing for tool calls and model calls
	switch event.Name {
	case hooks.EventToolStart:
		h.startSpan(event, spanName)
	case hooks.EventToolEnd:
		h.endSpan(event, spanName, hooks.EventToolStart)
	case hooks.EventModelStart:
		h.startSpan(event, spanName)
	case hooks.EventModelEnd:
		h.endSpan(event, spanName, hooks.EventModelStart)
	case hooks.EventAgentStart:
		h.startSpan(event, spanName)
	case hooks.EventAgentEnd:
		h.endSpan(event, spanName, hooks.EventAgentStart)
	default:
		// Single-shot events - create and immediately end span
		h.singleSpan(event, spanName)
	}

	return nil
}

// spanKey generates a unique key for tracking in-progress spans.
// For tool calls, we include the tool name to handle concurrent tool calls.
func (h *TelemetryHandler) spanKey(eventType, toolName string) string {
	if toolName != "" {
		return eventType + ":" + toolName
	}
	return eventType
}

// startSpan creates a new in-progress span.
func (h *TelemetryHandler) startSpan(event *hooks.Event, spanName string) {
	ctx := context.Background()
	attrs := h.eventToAttributes(event)

	ctx, span := h.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	h.emitLogRecord(ctx, event, spanName)

	key := h.spanKey(event.Name, event.Data.ToolName)
	h.spanStore.Store(key, &inProgressSpan{
		span:      span,
		ctx:       ctx,
		startTime: time.Now(),
		toolName:  event.Data.ToolName,
	})
}

// endSpan ends an in-progress span.
func (h *TelemetryHandler) endSpan(event *hooks.Event, spanName, startEventType string) {
	key := h.spanKey(startEventType, event.Data.ToolName)

	val, ok := h.spanStore.LoadAndDelete(key)
	if !ok {
		// No matching start event - create a single span
		h.singleSpan(event, spanName)
		return
	}

	inProgress := val.(*inProgressSpan)

	// Add end-event attributes
	attrs := h.eventToEndAttributes(event, inProgress.startTime)
	inProgress.span.SetAttributes(attrs...)

	// Set status based on success/error
	if event.Data.Error != "" {
		inProgress.span.SetStatus(codes.Error, event.Data.Error)
	} else if event.Data.Success {
		inProgress.span.SetStatus(codes.Ok, "")
	}

	h.emitLogRecord(inProgress.ctx, event, spanName)
	inProgress.span.End()

	// Record metrics for end events
	h.recordEndMetrics(event, startEventType, inProgress)
}

// singleSpan creates and immediately ends a span.
func (h *TelemetryHandler) singleSpan(event *hooks.Event, spanName string) {
	ctx := context.Background()
	attrs := h.eventToAttributes(event)

	// For session-end events, record session metric counters
	if event.Name == hooks.EventSessionEnd {
		h.recordSessionMetrics(event)
	}

	ctx, span := h.tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	h.emitLogRecord(ctx, event, spanName)

	// Set status based on success/error
	if event.Data.Error != "" {
		span.SetStatus(codes.Error, event.Data.Error)
	} else if event.Data.Success {
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}

// emitLogRecord emits a correlated log record for the event.
// The ctx must carry the active span so the otelslog bridge can extract
// trace_id and span_id for correlation.
func (h *TelemetryHandler) emitLogRecord(ctx context.Context, event *hooks.Event, spanName string) {
	if h.logger == nil {
		return
	}

	attrs := []slog.Attr{
		slog.String("event.name", event.Name),
	}

	if event.RawName != "" {
		attrs = append(attrs, slog.String("event.raw_name", event.RawName))
	}
	if event.Dialect != "" {
		attrs = append(attrs, slog.String("event.dialect", event.Dialect))
	}
	if event.Data.SessionID != "" {
		val := event.Data.SessionID
		if h.redactor != nil && h.redactor.ShouldHash("session_id") {
			val = telemetry.HashValue(val)
		}
		attrs = append(attrs, slog.String("session_id", val))
	}
	if event.Data.ToolName != "" {
		attrs = append(attrs, slog.String("tool_name", event.Data.ToolName))
	}
	if event.Data.ToolInput != "" {
		val := event.Data.ToolInput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_input") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, slog.String("tool_input", val))
	}
	if event.Data.ToolOutput != "" {
		val := event.Data.ToolOutput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_output") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, slog.String("tool_output", val))
	}
	if event.Data.Prompt != "" {
		val := event.Data.Prompt
		if h.redactor != nil && h.redactor.ShouldRedact("prompt") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, slog.String("prompt", val))
	}
	if event.Data.Source != "" {
		attrs = append(attrs, slog.String("source", event.Data.Source))
	}
	if event.Data.Reason != "" {
		attrs = append(attrs, slog.String("reason", event.Data.Reason))
	}
	if event.Data.Message != "" {
		attrs = append(attrs, slog.String("message", event.Data.Message))
	}
	if event.Data.Success {
		attrs = append(attrs, slog.Bool("success", true))
	}
	if event.Data.Error != "" {
		attrs = append(attrs, slog.String("error", event.Data.Error))
	}

	h.logger.LogAttrs(ctx, slog.LevelInfo, spanName, attrs...)
}

// eventToAttributes converts event data to span attributes.
func (h *TelemetryHandler) eventToAttributes(event *hooks.Event) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("event.name", event.Name),
	}

	if event.RawName != "" {
		attrs = append(attrs, attribute.String("event.raw_name", event.RawName))
	}
	if event.Dialect != "" {
		attrs = append(attrs, attribute.String("event.dialect", event.Dialect))
	}

	// Add data fields with redaction
	if event.Data.SessionID != "" {
		val := event.Data.SessionID
		if h.redactor != nil && h.redactor.ShouldHash("session_id") {
			val = telemetry.HashValue(val)
		}
		attrs = append(attrs, attribute.String("session_id", val))
	}

	if event.Data.ToolName != "" {
		attrs = append(attrs, attribute.String("tool_name", event.Data.ToolName))
	}

	if event.Data.ToolInput != "" {
		val := event.Data.ToolInput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_input") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_input", val))
	}

	if event.Data.ToolOutput != "" {
		val := event.Data.ToolOutput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_output") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_output", val))
	}

	if event.Data.Prompt != "" {
		val := event.Data.Prompt
		if h.redactor != nil && h.redactor.ShouldRedact("prompt") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("prompt", val))
	}

	if event.Data.Source != "" {
		attrs = append(attrs, attribute.String("source", event.Data.Source))
	}

	if event.Data.Reason != "" {
		attrs = append(attrs, attribute.String("reason", event.Data.Reason))
	}

	if event.Data.Message != "" {
		attrs = append(attrs, attribute.String("message", event.Data.Message))
	}

	return attrs
}

// eventToEndAttributes creates attributes specific to end events.
func (h *TelemetryHandler) eventToEndAttributes(event *hooks.Event, startTime time.Time) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int64("duration_ms", time.Since(startTime).Milliseconds()),
	}

	if event.Data.Success {
		attrs = append(attrs, attribute.Bool("success", true))
	}

	if event.Data.Error != "" {
		attrs = append(attrs, attribute.String("error", event.Data.Error))
	}

	// Add tool output for end events
	if event.Data.ToolOutput != "" {
		val := event.Data.ToolOutput
		if h.redactor != nil && h.redactor.ShouldRedact("tool_output") {
			val = "[REDACTED]"
		}
		attrs = append(attrs, attribute.String("tool_output", val))
	}

	return attrs
}

// metricAttrs returns common OTel metric attributes derived from environment.
func (h *TelemetryHandler) metricAttrs() []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if v := os.Getenv("SCION_AGENT_ID"); v != "" {
		attrs = append(attrs, attribute.String("agent_id", v))
	}
	if v := os.Getenv("SCION_HARNESS"); v != "" {
		attrs = append(attrs, attribute.String("harness", v))
	}
	return attrs
}

// recordEndMetrics records metrics when a paired end event completes.
func (h *TelemetryHandler) recordEndMetrics(event *hooks.Event, startEventType string, inProgress *inProgressSpan) {
	ctx := context.Background()
	durationMs := float64(time.Since(inProgress.startTime).Milliseconds())
	baseAttrs := h.metricAttrs()

	switch startEventType {
	case hooks.EventToolStart:
		if h.toolCalls != nil {
			status := "success"
			if event.Data.Error != "" {
				status = "error"
			}
			attrs := append(baseAttrs,
				attribute.String("tool_name", event.Data.ToolName),
				attribute.String("status", status),
			)
			h.toolCalls.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		if h.toolDuration != nil {
			attrs := append(baseAttrs,
				attribute.String("tool_name", event.Data.ToolName),
			)
			h.toolDuration.Record(ctx, durationMs, metric.WithAttributes(attrs...))
		}

	case hooks.EventModelStart:
		if h.apiCalls != nil {
			status := "success"
			if event.Data.Error != "" {
				status = "error"
			}
			attrs := baseAttrs
			if model := os.Getenv("SCION_MODEL"); model != "" {
				attrs = append(attrs, attribute.String("model", model))
			}
			attrs = append(attrs, attribute.String("status", status))
			h.apiCalls.Add(ctx, 1, metric.WithAttributes(attrs...))
		}
		if h.apiDuration != nil {
			attrs := baseAttrs
			if model := os.Getenv("SCION_MODEL"); model != "" {
				attrs = append(attrs, attribute.String("model", model))
			}
			h.apiDuration.Record(ctx, durationMs, metric.WithAttributes(attrs...))
		}
	}
}

// recordSessionMetrics records session counters on session end.
// Token usage metrics are populated via the native OTel pipeline from harnesses,
// not from Gemini session file parsing.
func (h *TelemetryHandler) recordSessionMetrics(event *hooks.Event) {
	if h.sessionCount == nil {
		return
	}

	ctx := context.Background()
	attrs := h.metricAttrs()

	status := "completed"
	if event.Data.Error != "" {
		status = "error"
	}
	attrs = append(attrs, attribute.String("status", status))
	h.sessionCount.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// Flush ends any in-progress spans. Called during shutdown.
func (h *TelemetryHandler) Flush() {
	h.spanStore.Range(func(key, value any) bool {
		if inProgress, ok := value.(*inProgressSpan); ok {
			inProgress.span.SetStatus(codes.Error, "span not properly ended - flushed during shutdown")
			inProgress.span.End()
		}
		h.spanStore.Delete(key)
		return true
	})
}
