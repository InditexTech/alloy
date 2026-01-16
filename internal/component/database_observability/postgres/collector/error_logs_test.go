package collector

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/alloy/internal/component/common/loki"
)

// TestErrorLogsCollector_ParseText tests parsing of stderr text format logs
func TestErrorLogsCollector_ParseText(t *testing.T) {
	tests := []struct {
		name        string
		textLog     string
		shouldParse bool
		checkFields func(*testing.T, prometheus.Gatherer)
	}{
		{
			name: "statement timeout",
			// Format: %m:%r:%u@%d:[%p]:%l:%e:%s:%v:%x:%c:%q%a
			textLog:     `2025-12-12 15:29:16.068 GMT:[local]:app-user@books_store:[9112]:4:57014:2025-12-12 15:29:15 GMT:25/112:0:693c34cb.2398::psqlERROR:  canceling statement due to statement timeout`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_total" {
						found = true
						require.Greater(t, len(mf.GetMetric()), 0)
						metric := mf.GetMetric()[0]
						labels := make(map[string]string)
						for _, label := range metric.GetLabel() {
							labels[label.GetName()] = label.GetValue()
						}
						require.Equal(t, "ERROR", labels["severity"])
						require.Equal(t, "books_store", labels["database"])
						require.Equal(t, "app-user", labels["user"])
						require.Equal(t, "test-system", labels["server_id"])
						require.Equal(t, "test-instance", labels["instance"])
					}
				}
				require.True(t, found, "metric should exist")
			},
		},
		{
			name:        "deadlock detected",
			textLog:     `2025-12-12 15:29:23.258 GMT:[local]:app-user@books_store:[9185]:9:40P01:2025-12-12 15:29:19 GMT:36/148:837:693c34cf.23e1::psqlERROR:  deadlock detected`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							if labels["user"] == "app-user" && labels["database"] == "books_store" {
								require.Equal(t, "ERROR", labels["severity"])
							}
						}
					}
				}
				require.True(t, found)
			},
		},
		{
			name:        "too many connections - FATAL severity",
			textLog:     `2025-12-12 15:29:31.529 GMT:[local]:conn_limited@books_store:[9449]:4:53300:2025-12-12 15:29:31 GMT:91/57:0:693c34db.24e9::psqlFATAL:  too many connections for role "conn_limited"`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							if labels["user"] == "conn_limited" {
								require.Equal(t, "FATAL", labels["severity"])
								require.Equal(t, "conn_limited", labels["user"])
							}
						}
					}
				}
				require.True(t, found)
			},
		},
		{
			name:        "authentication failure",
			textLog:     `2025-12-12 15:29:42.201 GMT:::1:app-user@books_store:[9589]:2:28P01:2025-12-12 15:29:42 GMT:159/363:0:693c34e6.2575::psqlFATAL:  password authentication failed for user "app-user"`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							// Check for the auth failure with app-user
							if labels["user"] == "app-user" && labels["severity"] == "FATAL" {
								require.Equal(t, "FATAL", labels["severity"])
								require.Equal(t, "books_store", labels["database"])
							}
						}
					}
				}
				require.True(t, found)
			},
		},
		{
			name:        "no SQLSTATE - should be skipped",
			textLog:     `2025-12-12 15:29:42.201 GMT:::1:app-user@books_store:[9589]:2::2025-12-12 15:29:42 GMT:159/363:0:693c34e6.2575::psqlLOG:  connection received`,
			shouldParse: false,
			checkFields: nil,
		},
		{
			name:        "INFO severity - should be skipped",
			textLog:     `2025-12-12 15:29:42.201 GMT:::1:app-user@books_store:[9589]:2:00000:2025-12-12 15:29:42 GMT:159/363:0:693c34e6.2575::psqlINFO:  some informational message`,
			shouldParse: false,
			checkFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})
			registry := prometheus.NewRegistry()

			collector, err := NewErrorLogs(ErrorLogsArguments{
				Receiver:     loki.NewLogsReceiver(),
				EntryHandler: entryHandler,
				Logger:       log.NewNopLogger(),
				InstanceKey:  "test-instance",
				SystemID:     "test-system",
				Registry:     registry,
			})
			require.NoError(t, err)

			err = collector.Start(context.Background())
			require.NoError(t, err)
			defer collector.Stop()

			// Send the log line
			collector.Receiver().Chan() <- loki.Entry{
				Entry: push.Entry{
					Line:      tt.textLog,
					Timestamp: time.Now(),
				},
			}

			time.Sleep(100 * time.Millisecond)

			if tt.checkFields != nil {
				tt.checkFields(t, registry)
			}
		})
	}
}

func TestErrorLogsCollector_StartStop(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:     loki.NewLogsReceiver(),
		EntryHandler: entryHandler,
		Logger:       log.NewNopLogger(),
		InstanceKey:  "test",
		SystemID:     "test",
		Registry:     prometheus.NewRegistry(),
	})
	require.NoError(t, err)
	require.NotNil(t, collector)
	require.NotNil(t, collector.Receiver(), "receiver should be exported")

	err = collector.Start(context.Background())
	require.NoError(t, err)
	require.False(t, collector.Stopped())

	time.Sleep(10 * time.Millisecond)

	collector.Stop()

	time.Sleep(10 * time.Millisecond)
	require.True(t, collector.Stopped())
}

func TestErrorLogsCollector_ExtractSeverity(t *testing.T) {
	tests := []struct {
		message  string
		expected string
	}{
		{
			message:  "ERROR:  canceling statement due to statement timeout",
			expected: "ERROR",
		},
		{
			message:  "FATAL:  too many connections",
			expected: "FATAL",
		},
		{
			message:  "PANIC:  could not write to file",
			expected: "PANIC",
		},
		{
			message:  "LOG:  connection received",
			expected: "LOG",
		},
		{
			message:  "WARNING:  deprecated syntax",
			expected: "WARNING",
		},
		{
			message:  "no colon here",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractSeverity(tt.message)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestErrorLogsCollector_IsContinuationLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "tab-indented line",
			line:     "\tThis probably means the server terminated abnormally",
			expected: true,
		},
		{
			name:     "DETAIL line",
			line:     "DETAIL:  Key (author_id)=(99999) is not present in table \"authors\".",
			expected: true,
		},
		{
			name:     "HINT line",
			line:     "HINT:  Check the foreign key constraint.",
			expected: true,
		},
		{
			name:     "CONTEXT line",
			line:     "CONTEXT:  SQL statement \"SELECT * FROM books WHERE author_id = 123\"",
			expected: true,
		},
		{
			name:     "STATEMENT line",
			line:     "STATEMENT:  INSERT INTO books (title, author_id) VALUES ('Test', 99999)",
			expected: true,
		},
		{
			name:     "QUERY line",
			line:     "QUERY:  SELECT 1",
			expected: true,
		},
		{
			name:     "LOCATION line",
			line:     "LOCATION:  postgres.c:1234",
			expected: true,
		},
		{
			name:     "DETAIL with leading whitespace",
			line:     "  DETAIL:  Some detail text",
			expected: true,
		},
		{
			name:     "regular log line",
			line:     "2025-12-12 15:29:23.258 GMT|app-user|books_store|[local]|9185|9|57014|2025-12-12 15:29:19 GMT|36/148|837|693c34cf.23e1|SELECT|0|psql|5457019535816659310|ERROR:  canceling statement due to statement timeout",
			expected: false,
		},
		{
			name:     "empty line",
			line:     "",
			expected: false,
		},
		{
			name:     "error in continuation keyword",
			line:     "ERROR:  some error message",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContinuationLine(tt.line)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestErrorLogsCollector_InvalidLogFormat(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})
	registry := prometheus.NewRegistry()

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:     loki.NewLogsReceiver(),
		EntryHandler: entryHandler,
		Logger:       log.NewNopLogger(),
		InstanceKey:  "test",
		SystemID:     "test",
		Registry:     registry,
	})
	require.NoError(t, err)

	err = collector.Start(context.Background())
	require.NoError(t, err)
	defer collector.Stop()

	// Send an invalid log line (not enough fields)
	collector.Receiver().Chan() <- loki.Entry{
		Entry: push.Entry{
			Line:      `invalid|log|line`,
			Timestamp: time.Now(),
		},
	}

	time.Sleep(100 * time.Millisecond)

	// Check that parse errors counter was incremented
	mfs, _ := registry.Gather()
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "postgres_error_log_parse_failures_total" {
			found = true
			require.Greater(t, mf.GetMetric()[0].GetCounter().GetValue(), 0.0)
		}
	}
	require.True(t, found, "parse error metric should exist")
}

func TestErrorLogsCollector_ContinuationLinesDoNotIncrementParseFailures(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})
	registry := prometheus.NewRegistry()

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:     loki.NewLogsReceiver(),
		EntryHandler: entryHandler,
		Logger:       log.NewNopLogger(),
		InstanceKey:  "test",
		SystemID:     "test-system-id",
		Registry:     registry,
	})
	require.NoError(t, err)

	err = collector.Start(context.Background())
	require.NoError(t, err)
	defer collector.Stop()

	// Send continuation lines (should NOT increment parse failures)
	continuationLines := []string{
		"\tThis probably means the server terminated abnormally",
		"DETAIL:  Key (author_id)=(99999) is not present in table \"authors\".",
		"HINT:  Check the foreign key constraint.",
		"CONTEXT:  SQL statement \"SELECT * FROM books WHERE author_id = 123\"",
		"STATEMENT:  INSERT INTO books (title, author_id) VALUES ('Test', 99999)",
	}

	for _, line := range continuationLines {
		collector.Receiver().Chan() <- loki.Entry{
			Entry: push.Entry{
				Line:      line,
				Timestamp: time.Now(),
			},
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Check that parse errors counter was NOT incremented (should be 0)
	mfs, _ := registry.Gather()
	parseFailuresFound := false
	for _, mf := range mfs {
		if mf.GetName() == "postgres_error_log_parse_failures_total" {
			parseFailuresFound = true
			// Should be 0 since continuation lines don't count as parse failures
			require.Equal(t, 0.0, mf.GetMetric()[0].GetCounter().GetValue())
		}
	}

	// The metric should exist but be zero
	require.True(t, parseFailuresFound, "parse failures metric should exist")
}

// TestErrorLogsCollector_RDSLikeLogs tests with comprehensive RDS-like log samples
func TestErrorLogsCollector_RDSLikeLogs(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 100), func() {})
	registry := prometheus.NewRegistry()

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:     loki.NewLogsReceiver(),
		EntryHandler: entryHandler,
		Logger:       log.NewNopLogger(),
		InstanceKey:  "rds-test-instance",
		SystemID:     "rds-system",
		Registry:     registry,
	})
	require.NoError(t, err)

	err = collector.Start(context.Background())
	require.NoError(t, err)
	defer collector.Stop()

	// Real RDS-like log samples covering various error types
	// Format: %m:%r:%u@%d:[%p]:%l:%e:%s:%v:%x:%c:%q%a
	logSamples := []struct {
		log      string
		user     string
		database string
		severity string
	}{
		{
			log:      `2025-01-12 10:30:45.123 UTC:10.0.1.5:54321:app-user@books_store:[9112]:4:57014:2025-01-12 10:29:15 UTC:25/112:0:693c34cb.2398::psqlERROR:  canceling statement due to statement timeout`,
			user:     "app-user",
			database: "books_store",
			severity: "ERROR",
		},
		{
			log:      `2025-01-12 10:31:23.258 UTC:10.0.1.5:54321:app-user@books_store:[9185]:9:40P01:2025-01-12 10:29:19 UTC:36/148:837:693c34cf.23e1::webappERROR:  deadlock detected`,
			user:     "app-user",
			database: "books_store",
			severity: "ERROR",
		},
		{
			log:      `2025-01-12 10:32:31.529 UTC:10.0.1.10:45678:conn_limited@books_store:[9449]:4:53300:2025-01-12 10:32:31 UTC:91/57:0:693c34db.24e9::api_workerFATAL:  too many connections for role "conn_limited"`,
			user:     "conn_limited",
			database: "books_store",
			severity: "FATAL",
		},
		{
			log:      `2025-01-12 10:33:42.201 UTC:10.0.1.20:52860:app-user@books_store:[9589]:2:28P01:2025-01-12 10:33:42 UTC:159/363:0:693c34e6.2575::jdbc_clientFATAL:  password authentication failed for user "app-user"`,
			user:     "app-user",
			database: "books_store",
			severity: "FATAL",
		},
		{
			log:      `2025-01-12 10:34:15.891 UTC:10.0.1.5:54322:web-user@orders_db:[10123]:7:23505:2025-01-12 10:30:00 UTC:42/201:1045:693c3500.2790::web_appERROR:  duplicate key value violates unique constraint "orders_pkey"`,
			user:     "web-user",
			database: "orders_db",
			severity: "ERROR",
		},
		{
			log:      `2025-01-12 10:35:22.456 UTC:10.0.1.8:43210:api-user@products_db:[10456]:5:23503:2025-01-12 10:35:00 UTC:55/89:1123:693c3512.28d8::rest_apiERROR:  insert or update on table "order_items" violates foreign key constraint "order_items_product_id_fkey"`,
			user:     "api-user",
			database: "products_db",
			severity: "ERROR",
		},
		{
			log:      `2025-01-12 10:36:10.789 UTC:10.0.1.15:54323:analytics-user@reports_db:[11234]:3:22012:2025-01-12 10:35:45 UTC:67/145:0:693c3520.2be2::analytics_toolERROR:  division by zero`,
			user:     "analytics-user",
			database: "reports_db",
			severity: "ERROR",
		},
		{
			log:      `2025-01-12 10:42:50.789 UTC:10.0.1.50:54329:app-user@app_db:[13012]:4:42P01:2025-01-12 10:40:00 UTC:134/789:2001:693c3580.32d4::app_serverERROR:  relation "non_existent_table" does not exist`,
			user:     "app-user",
			database: "app_db",
			severity: "ERROR",
		},
	}

	// Send all log samples
	for _, sample := range logSamples {
		collector.Receiver().Chan() <- loki.Entry{
			Entry: push.Entry{
				Line:      sample.log,
				Timestamp: time.Now(),
			},
		}
	}

	time.Sleep(200 * time.Millisecond)

	// Verify metrics were created for all expected error types
	mfs, _ := registry.Gather()
	var errorMetrics *dto.MetricFamily
	for _, mf := range mfs {
		if mf.GetName() == "postgres_errors_total" {
			errorMetrics = mf
			break
		}
	}

	require.NotNil(t, errorMetrics, "error metrics should exist")
	require.GreaterOrEqual(t, len(errorMetrics.GetMetric()), len(logSamples), "should have metrics for all error types")

	// Verify that all expected user/database/severity combinations were captured
	type metricKey struct {
		user     string
		database string
		severity string
	}
	capturedMetrics := make(map[metricKey]bool)

	for _, metric := range errorMetrics.GetMetric() {
		labels := make(map[string]string)
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}

		key := metricKey{
			user:     labels["user"],
			database: labels["database"],
			severity: labels["severity"],
		}
		capturedMetrics[key] = true

		// Verify instance and server_id labels are set correctly
		require.Equal(t, "rds-test-instance", labels["instance"])
		require.Equal(t, "rds-system", labels["server_id"])
	}

	// Verify all expected samples were captured
	for _, sample := range logSamples {
		key := metricKey{
			user:     sample.user,
			database: sample.database,
			severity: sample.severity,
		}
		require.True(t, capturedMetrics[key], "Expected metric for user=%s, database=%s, severity=%s",
			sample.user, sample.database, sample.severity)
	}

	// Verify logs without SQLSTATE or with INFO/LOG severity were skipped
	collector.Receiver().Chan() <- loki.Entry{
		Entry: push.Entry{
			Line:      `2025-01-12 10:45:00.000 UTC:10.0.1.5:54321:app-user@books_store:[9112]:5::2025-01-12 10:29:15 UTC:25/112:0:693c34cb.2398::psqlLOG:  connection received`,
			Timestamp: time.Now(),
		},
	}

	time.Sleep(100 * time.Millisecond)

	// Metric count should not have increased
	mfsAfter, _ := registry.Gather()
	for _, mf := range mfsAfter {
		if mf.GetName() == "postgres_errors_total" {
			require.Equal(t, len(errorMetrics.GetMetric()), len(mf.GetMetric()), "should not create metrics for non-error logs")
		}
	}
}
