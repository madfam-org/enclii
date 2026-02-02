package logging

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Logger interface
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...Field)
	Info(ctx context.Context, msg string, fields ...Field)
	Warn(ctx context.Context, msg string, fields ...Field)
	Error(ctx context.Context, msg string, fields ...Field)
	Fatal(ctx context.Context, msg string, fields ...Field)

	WithField(key string, value interface{}) Logger
	WithFields(fields Fields) Logger
	WithError(err error) Logger
	WithContext(ctx context.Context) Logger
}

// Field represents a structured logging field
type Field struct {
	Key   string
	Value interface{}
}

// Fields is a map of field keys to values
type Fields map[string]interface{}

// StructuredLogger implements the Logger interface
type StructuredLogger struct {
	logger *logrus.Logger
	fields logrus.Fields
	ctx    context.Context
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level       string `json:"level"`
	Format      string `json:"format"` // "json" or "text"
	Output      string `json:"output"` // "stdout", "stderr", or file path
	ServiceName string `json:"service_name"`
	Version     string `json:"version"`
	Environment string `json:"environment"`

	// Tracing configuration
	TracingEnabled bool    `json:"tracing_enabled"`
	JaegerEndpoint string  `json:"jaeger_endpoint"`
	TracingSampler float64 `json:"tracing_sampler"`
}

// Request ID middleware key
const RequestIDKey = "request_id"
const UserIDKey = "user_id"
const TraceIDKey = "trace_id"
const SpanIDKey = "span_id"

var (
	defaultLogger Logger
	tracer        oteltrace.Tracer
)

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger(config *LogConfig) (Logger, error) {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// Set formatter
	if config.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyFunc:  "function",
			},
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: time.RFC3339,
			FullTimestamp:   true,
		})
	}

	// Set output
	switch config.Output {
	case "stderr":
		logger.SetOutput(os.Stderr)
	case "stdout":
		logger.SetOutput(os.Stdout)
	default:
		if config.Output != "" {
			file, err := os.OpenFile(config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				return nil, fmt.Errorf("failed to open log file: %w", err)
			}
			logger.SetOutput(file)
		} else {
			logger.SetOutput(os.Stdout)
		}
	}

	// Add default fields
	defaultFields := logrus.Fields{
		"service":     config.ServiceName,
		"version":     config.Version,
		"environment": config.Environment,
	}

	structuredLogger := &StructuredLogger{
		logger: logger,
		fields: defaultFields,
	}

	// Initialize tracing if enabled
	if config.TracingEnabled {
		if err := initTracing(config); err != nil {
			structuredLogger.Warn(context.Background(), "Failed to initialize tracing", Field{Key: "error", Value: err.Error()})
		} else {
			tracer = otel.Tracer(config.ServiceName)
		}
	}

	defaultLogger = structuredLogger
	return structuredLogger, nil
}

func initTracing(config *LogConfig) error {
	ctx := context.Background()

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(config.JaegerEndpoint),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize OTLP trace exporter: %w", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.Version),
			semconv.DeploymentEnvironmentKey.String(config.Environment),
		)),
		trace.WithSampler(trace.TraceIDRatioBased(config.TracingSampler)),
	)

	otel.SetTracerProvider(tp)
	return nil
}

func (l *StructuredLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, logrus.DebugLevel, msg, fields...)
}

func (l *StructuredLogger) Info(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, logrus.InfoLevel, msg, fields...)
}

func (l *StructuredLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, logrus.WarnLevel, msg, fields...)
}

func (l *StructuredLogger) Error(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, logrus.ErrorLevel, msg, fields...)
}

func (l *StructuredLogger) Fatal(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, logrus.FatalLevel, msg, fields...)
}

func (l *StructuredLogger) log(ctx context.Context, level logrus.Level, msg string, fields ...Field) {
	entry := l.logger.WithFields(l.fields)

	// Add context fields
	if ctx != nil {
		if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
			entry = entry.WithField("request_id", requestID)
		}

		if userID, ok := ctx.Value(UserIDKey).(string); ok {
			entry = entry.WithField("user_id", userID)
		}

		// Add tracing information
		if span := oteltrace.SpanFromContext(ctx); span.SpanContext().IsValid() {
			spanContext := span.SpanContext()
			entry = entry.WithFields(logrus.Fields{
				"trace_id": spanContext.TraceID().String(),
				"span_id":  spanContext.SpanID().String(),
			})
		}
	}

	// Add caller information
	if pc, file, line, ok := runtime.Caller(2); ok {
		funcName := runtime.FuncForPC(pc).Name()
		entry = entry.WithFields(logrus.Fields{
			"caller":   fmt.Sprintf("%s:%d", file, line),
			"function": funcName,
		})
	}

	// Add custom fields
	for _, field := range fields {
		entry = entry.WithField(field.Key, field.Value)
	}

	entry.Log(level, msg)

	// Add to trace span if available
	if ctx != nil && level >= logrus.ErrorLevel {
		if span := oteltrace.SpanFromContext(ctx); span.SpanContext().IsValid() {
			span.SetStatus(codes.Error, msg)
			span.SetAttributes(attribute.String("log.level", level.String()))
		}
	}
}

func (l *StructuredLogger) WithField(key string, value interface{}) Logger {
	newFields := make(logrus.Fields)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value

	return &StructuredLogger{
		logger: l.logger,
		fields: newFields,
		ctx:    l.ctx,
	}
}

func (l *StructuredLogger) WithFields(fields Fields) Logger {
	newFields := make(logrus.Fields)
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

	return &StructuredLogger{
		logger: l.logger,
		fields: newFields,
		ctx:    l.ctx,
	}
}

func (l *StructuredLogger) WithError(err error) Logger {
	return l.WithField("error", err.Error())
}

func (l *StructuredLogger) WithContext(ctx context.Context) Logger {
	return &StructuredLogger{
		logger: l.logger,
		fields: l.fields,
		ctx:    ctx,
	}
}

// Middleware functions
func RequestLoggingMiddleware(logger Logger) gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		ctx := param.Request.Context()

		fields := []Field{
			{Key: "client_ip", Value: param.ClientIP},
			{Key: "method", Value: param.Method},
			{Key: "path", Value: param.Path},
			{Key: "status_code", Value: param.StatusCode},
			{Key: "latency", Value: param.Latency.String()},
			{Key: "user_agent", Value: param.Request.UserAgent()},
			{Key: "request_size", Value: param.Request.ContentLength},
			{Key: "response_size", Value: param.BodySize},
		}

		if param.ErrorMessage != "" {
			fields = append(fields, Field{Key: "error", Value: param.ErrorMessage})
		}

		// Log based on status code
		if param.StatusCode >= 500 {
			logger.Error(ctx, "HTTP request completed", fields...)
		} else if param.StatusCode >= 400 {
			logger.Warn(ctx, "HTTP request completed", fields...)
		} else {
			logger.Info(ctx, "HTTP request completed", fields...)
		}

		return ""
	})
}

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set(RequestIDKey, requestID)
		c.Header("X-Request-ID", requestID)

		// Add to context
		ctx := context.WithValue(c.Request.Context(), RequestIDKey, requestID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if tracer == nil {
			c.Next()
			return
		}

		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.FullPath())
		ctx, span := tracer.Start(c.Request.Context(), spanName)
		defer span.End()

		// Set span attributes
		span.SetAttributes(
			semconv.HTTPMethodKey.String(c.Request.Method),
			semconv.HTTPURLKey.String(c.Request.URL.String()),
			semconv.HTTPUserAgentKey.String(c.Request.UserAgent()),
			semconv.HTTPClientIPKey.String(c.ClientIP()),
		)

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		// Set response attributes
		span.SetAttributes(
			semconv.HTTPStatusCodeKey.Int(c.Writer.Status()),
			attribute.Int("http.response_content_length", c.Writer.Size()),
		)

		// Set span status based on HTTP status
		if c.Writer.Status() >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", c.Writer.Status()))
		}
	}
}

// Utility functions
func GetLogger() Logger {
	return defaultLogger
}

func SetLogger(logger Logger) {
	defaultLogger = logger
}

// Helper functions for creating fields
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value.String()}
}

func Error(key string, err error) Field {
	return Field{Key: key, Value: err.Error()}
}

// Tracing helpers
func StartSpan(ctx context.Context, operationName string) (context.Context, oteltrace.Span) {
	if tracer == nil {
		return ctx, oteltrace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, operationName)
}

func AddSpanAttribute(span oteltrace.Span, key string, value interface{}) {
	switch v := value.(type) {
	case string:
		span.SetAttributes(attribute.String(key, v))
	case int:
		span.SetAttributes(attribute.Int(key, v))
	case int64:
		span.SetAttributes(attribute.Int64(key, v))
	case float64:
		span.SetAttributes(attribute.Float64(key, v))
	case bool:
		span.SetAttributes(attribute.Bool(key, v))
	default:
		span.SetAttributes(attribute.String(key, fmt.Sprintf("%v", v)))
	}
}

func SetSpanError(span oteltrace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// Log level utilities
func ParseLogLevel(level string) logrus.Level {
	parsedLevel, err := logrus.ParseLevel(level)
	if err != nil {
		return logrus.InfoLevel
	}
	return parsedLevel
}

func GetLogLevel() logrus.Level {
	if structuredLogger, ok := defaultLogger.(*StructuredLogger); ok {
		return structuredLogger.logger.Level
	}
	return logrus.InfoLevel
}

func SetLogLevel(level logrus.Level) {
	if structuredLogger, ok := defaultLogger.(*StructuredLogger); ok {
		structuredLogger.logger.SetLevel(level)
	}
}

// Default configuration
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:          "info",
		Format:         "json",
		Output:         "stdout",
		ServiceName:    "enclii-switchyard",
		Version:        "0.1.0",
		Environment:    "development",
		TracingEnabled: true,
		JaegerEndpoint: "http://localhost:4318/v1/traces",
		TracingSampler: 0.1, // 10% sampling
	}
}

// Context utilities
func LoggerFromContext(ctx context.Context) Logger {
	return defaultLogger.WithContext(ctx)
}

func FieldsFromGinContext(c *gin.Context) Fields {
	fields := Fields{}

	if requestID, exists := c.Get(RequestIDKey); exists {
		fields["request_id"] = requestID
	}

	if userID, exists := c.Get(UserIDKey); exists {
		fields["user_id"] = userID
	}

	return fields
}
