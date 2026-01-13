package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/alloy/internal/component/common/loki"
	"github.com/grafana/alloy/internal/runtime/logging/level"
)

const (
	ErrorLogsCollector = "error_logs"
	OP_ERROR_LOGS      = "error_logs"
)

// Supported error severities that will be processed
var supportedSeverities = map[string]bool{
	"ERROR": true,
	"FATAL": true,
	"PANIC": true,
}

// PostgreSQL Text Log Format (stderr)
// Expected log_line_prefix: %m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|
// This produces 15 pipe-delimited fields followed by the log message.
//
// Field mapping:
// 1. %m - Timestamp with milliseconds
// 2. %u - User name
// 3. %d - Database name
// 4. %r - Remote host:port
// 5. %p - Process ID
// 6. %l - Session line number
// 7. %e - SQLSTATE error code
// 8. %s - Session start timestamp
// 9. %v - Virtual transaction ID
// 10. %x - Transaction ID
// 11. %c - Session ID
// 12. %i - Command tag (ps)
// 13. %P - Parallel leader PID
// 14. %a - Application name
// 15. %Q - Query ID (requires PostgreSQL 14+, compute_query_id = on)
// 16. Log message (severity: message text)

// ParsedError contains the extracted error information.
// Phase 1: Only fields needed for metrics are populated.
// Phase 2 (future): All fields will be populated for full Loki log emission.
type ParsedError struct {
	// Phase 1 fields (used for metrics)
	ErrorSeverity string // ERROR, FATAL, PANIC
	SQLState      string // SQLSTATE code (e.g., "57014")
	ErrorName     string // Human-readable error name (e.g., "query_canceled")
	SQLStateClass string // First 2 chars of SQLSTATE (e.g., "57")
	ErrorCategory string // Error category (e.g., "Operator Intervention")
	User          string // Database user
	DatabaseName  string // Database name
	QueryID       int64  // Query ID (from %Q, requires PG 14+)

	// Phase 2 fields (deferred - not yet populated in Phase 1)
	Timestamp        time.Time
	PID              int32
	SessionID        string
	LineNum          int32
	RemoteHost       string
	RemotePort       int32
	ApplicationName  string
	BackendType      string
	PS               string
	SessionStart     time.Time
	VXID             string
	TXID             string
	Message          string
	Detail           string
	Hint             string
	Context          string
	Statement        string
	CursorPosition   int32
	InternalQuery    string
	InternalPosition int32
	FuncName         string
	FileName         string
	FileLineNum      int32
	LeaderPID        int32
}

type ErrorLogsArguments struct {
	Receiver              loki.LogsReceiver
	EntryHandler          loki.EntryHandler
	Logger                log.Logger
	InstanceKey           string
	SystemID              string
	Registry              *prometheus.Registry
	DisableQueryRedaction bool
}

type ErrorLogs struct {
	logger                log.Logger
	entryHandler          loki.EntryHandler
	instanceKey           string
	systemID              string
	registry              *prometheus.Registry
	disableQueryRedaction bool

	receiver loki.LogsReceiver

	errorsBySQLState *prometheus.CounterVec
	parseErrors      prometheus.Counter

	ctx     context.Context
	cancel  context.CancelFunc
	stopped *atomic.Bool
	wg      sync.WaitGroup
}

func NewErrorLogs(args ErrorLogsArguments) (*ErrorLogs, error) {
	ctx, cancel := context.WithCancel(context.Background())

	e := &ErrorLogs{
		logger:                log.With(args.Logger, "collector", ErrorLogsCollector),
		entryHandler:          args.EntryHandler,
		instanceKey:           args.InstanceKey,
		systemID:              args.SystemID,
		registry:              args.Registry,
		disableQueryRedaction: args.DisableQueryRedaction,
		receiver:              args.Receiver,
		ctx:                   ctx,
		cancel:                cancel,
		stopped:               atomic.NewBool(false),
	}

	e.initMetrics()

	return e, nil
}

func (c *ErrorLogs) initMetrics() {
	c.errorsBySQLState = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "postgres_errors_by_sqlstate_query_user_total",
			Help: "PostgreSQL errors by SQLSTATE code with database, user, queryid, and instance tracking",
		},
		[]string{"sqlstate", "error_name", "sqlstate_class", "error_category", "severity", "database", "user", "queryid", "instance"},
	)

	c.parseErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "postgres_error_log_parse_failures_total",
			Help: "Failed to parse log lines",
		},
	)

	if c.registry != nil {
		c.registry.MustRegister(
			c.errorsBySQLState,
			c.parseErrors,
		)
	} else {
		level.Warn(c.logger).Log("msg", "no Prometheus registry provided, metrics will not be exposed")
	}
}

func (c *ErrorLogs) Name() string {
	return ErrorLogsCollector
}

// Receiver returns the logs receiver that loki.source.* can forward to
func (c *ErrorLogs) Receiver() loki.LogsReceiver {
	return c.receiver
}

func (c *ErrorLogs) Start(ctx context.Context) error {
	level.Debug(c.logger).Log("msg", "collector started")

	c.wg.Add(1)
	go c.run()
	return nil
}

func (c *ErrorLogs) Stop() {
	c.cancel()
	c.stopped.Store(true)
	c.wg.Wait()
}

func (c *ErrorLogs) Stopped() bool {
	return c.stopped.Load()
}

func (c *ErrorLogs) run() {
	defer c.wg.Done()

	level.Debug(c.logger).Log("msg", "collector running, waiting for log entries")

	for {
		select {
		case <-c.ctx.Done():
			level.Debug(c.logger).Log("msg", "collector stopping")
			return
		case entry := <-c.receiver.Chan():
			if err := c.processLogLine(entry); err != nil {
				level.Warn(c.logger).Log(
					"msg", "failed to process log line",
					"error", err,
					"line_preview", truncateString(entry.Entry.Line, 100),
				)
			}
		}
	}
}

func (c *ErrorLogs) processLogLine(entry loki.Entry) error {
	// Phase 1: Parse text format for metrics only
	return c.parseTextLog(entry)
}

// parseTextLog extracts fields from stderr text format logs for Phase 1 metrics.
// Expected format: %m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|SEVERITY:  message
func (c *ErrorLogs) parseTextLog(entry loki.Entry) error {
	line := entry.Entry.Line

	// Split into 16 parts: 15 prefix fields + message
	parts := strings.SplitN(line, "|", 16)
	if len(parts) < 16 {
		c.parseErrors.Inc()
		return fmt.Errorf("invalid log line format: expected 16 pipe-delimited fields, got %d", len(parts))
	}

	// Extract ONLY the 5 fields needed for Phase 1 metrics
	user := strings.TrimSpace(parts[1])        // Field 2: %u (user)
	database := strings.TrimSpace(parts[2])    // Field 3: %d (database)
	sqlstate := strings.TrimSpace(parts[6])    // Field 7: %e (SQLSTATE)
	queryIDStr := strings.TrimSpace(parts[14]) // Field 15: %Q (query_id)
	messageAndRest := parts[15]                // Field 16: severity + message

	// Parse severity from the message part (e.g., "ERROR:  message text")
	severity := extractSeverity(messageAndRest)

	// Filter: only process ERROR, FATAL, PANIC
	if !supportedSeverities[severity] {
		return nil // Skip INFO, LOG, WARNING, etc.
	}

	// Filter: skip if no SQLSTATE (can't categorize the error)
	if sqlstate == "" {
		return nil
	}

	// Parse query_id (may be 0 if not available)
	queryID, _ := strconv.ParseInt(queryIDStr, 10, 64)

	// Use existing helper functions to get error metadata
	errorName := GetSQLStateErrorName(sqlstate)
	sqlstateClass := ""
	if len(sqlstate) >= 2 {
		sqlstateClass = sqlstate[:2]
	}
	errorCategory := GetSQLStateCategory(sqlstate)

	// Create minimal ParsedError for Phase 1
	parsed := &ParsedError{
		ErrorSeverity: severity,
		SQLState:      sqlstate,
		ErrorName:     errorName,
		SQLStateClass: sqlstateClass,
		ErrorCategory: errorCategory,
		User:          user,
		DatabaseName:  database,
		QueryID:       queryID,
	}

	// Emit metrics only (Phase 1)
	c.updateMetrics(parsed)

	return nil
}

// extractSeverity parses the severity from the message part.
// Input: "ERROR:  canceling statement due to timeout"
// Output: "ERROR"
func extractSeverity(message string) string {
	// Format is typically "SEVERITY:  message text"
	if idx := strings.Index(message, ":"); idx > 0 {
		return strings.TrimSpace(message[:idx])
	}
	return ""
}

func (c *ErrorLogs) updateMetrics(parsed *ParsedError) {
	// Only emit metrics if we have a valid SQLSTATE
	if parsed.SQLState == "" {
		return
	}

	// Convert queryID to string for metric label
	queryIDStr := ""
	if parsed.QueryID > 0 {
		queryIDStr = strconv.FormatInt(parsed.QueryID, 10)
	}

	c.errorsBySQLState.WithLabelValues(
		parsed.SQLState,      // sqlstate: "57014"
		parsed.ErrorName,     // error_name: "query_canceled"
		parsed.SQLStateClass, // sqlstate_class: "57"
		parsed.ErrorCategory, // error_category: "Operator Intervention"
		parsed.ErrorSeverity, // severity: "ERROR"
		parsed.DatabaseName,  // database: "books_store"
		parsed.User,          // user: "app-user"
		queryIDStr,           // queryid: "5457019535816659310"
		c.instanceKey,        // instance: "orders_db"
	).Inc()
}

// Phase 2: Loki log emission will be implemented here
// For now, Phase 1 only emits metrics

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
