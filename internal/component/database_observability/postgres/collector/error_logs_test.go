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
			name: "statement timeout (57014) with query_id",
			// Format: %m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|MESSAGE
			textLog:     `2025-12-12 15:29:16.068 GMT|app-user|books_store|[local]|9112|4|57014|2025-12-12 15:29:15 GMT|25/112|0|693c34cb.2398|SELECT|0|psql|5457019535816659310|ERROR:  canceling statement due to statement timeout`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
						found = true
						require.Greater(t, len(mf.GetMetric()), 0)
						metric := mf.GetMetric()[0]
						labels := make(map[string]string)
						for _, label := range metric.GetLabel() {
							labels[label.GetName()] = label.GetValue()
						}
						require.Equal(t, "57014", labels["sqlstate"])
						require.Equal(t, "query_canceled", labels["error_name"])
						require.Equal(t, "57", labels["sqlstate_class"])
						require.Equal(t, "Operator Intervention", labels["error_category"])
						require.Equal(t, "ERROR", labels["severity"])
						require.Equal(t, "books_store", labels["database"])
						require.Equal(t, "app-user", labels["user"])
						require.Equal(t, "5457019535816659310", labels["queryid"])
					}
				}
				require.True(t, found, "metric should exist")
			},
		},
		{
			name:        "deadlock detected (40P01)",
			textLog:     `2025-12-12 15:29:23.258 GMT|app-user|books_store|[local]|9185|9|40P01|2025-12-12 15:29:19 GMT|36/148|837|693c34cf.23e1|UPDATE|0|psql|3188095831510673590|ERROR:  deadlock detected`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							if labels["sqlstate"] == "40P01" {
								require.Equal(t, "deadlock_detected", labels["error_name"])
								require.Equal(t, "40", labels["sqlstate_class"])
								require.Equal(t, "Transaction Rollback", labels["error_category"])
								require.Equal(t, "ERROR", labels["severity"])
							}
						}
					}
				}
				require.True(t, found)
			},
		},
		{
			name:        "too many connections (53300) - FATAL severity",
			textLog:     `2025-12-12 15:29:31.529 GMT|conn_limited|books_store|[local]|9449|4|53300|2025-12-12 15:29:31 GMT|91/57|0|693c34db.24e9|startup|0|psql|6883023751393440299|FATAL:  too many connections for role "conn_limited"`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							if labels["sqlstate"] == "53300" {
								require.Equal(t, "too_many_connections", labels["error_name"])
								require.Equal(t, "53", labels["sqlstate_class"])
								require.Equal(t, "Insufficient Resources", labels["error_category"])
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
			name:        "authentication failure (28P01)",
			textLog:     `2025-12-12 15:29:42.201 GMT|app-user|books_store|::1|9589|2|28P01|2025-12-12 15:29:42 GMT|159/363|0|693c34e6.2575|authentication|0|psql|225649433808025698|FATAL:  password authentication failed for user "app-user"`,
			shouldParse: true,
			checkFields: func(t *testing.T, g prometheus.Gatherer) {
				mfs, _ := g.Gather()
				found := false
				for _, mf := range mfs {
					if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
						found = true
						for _, metric := range mf.GetMetric() {
							labels := make(map[string]string)
							for _, label := range metric.GetLabel() {
								labels[label.GetName()] = label.GetValue()
							}
							if labels["sqlstate"] == "28P01" {
								require.Equal(t, "invalid_password", labels["error_name"])
								require.Equal(t, "28", labels["sqlstate_class"])
								require.Equal(t, "Invalid Authorization Specification", labels["error_category"])
								require.Equal(t, "FATAL", labels["severity"])
							}
						}
					}
				}
				require.True(t, found)
			},
		},
		{
			name:        "no SQLSTATE - should be skipped",
			textLog:     `2025-12-12 15:29:42.201 GMT|app-user|books_store|::1|9589|2||2025-12-12 15:29:42 GMT|159/363|0|693c34e6.2575|idle|0|psql|0|LOG:  connection received`,
			shouldParse: false,
			checkFields: nil,
		},
		{
			name:        "INFO severity - should be skipped",
			textLog:     `2025-12-12 15:29:42.201 GMT|app-user|books_store|::1|9589|2|00000|2025-12-12 15:29:42 GMT|159/363|0|693c34e6.2575|idle|0|psql|0|INFO:  some informational message`,
			shouldParse: false,
			checkFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})
			registry := prometheus.NewRegistry()

			collector, err := NewErrorLogs(ErrorLogsArguments{
				Receiver:              loki.NewLogsReceiver(),
				EntryHandler:          entryHandler,
				Logger:                log.NewNopLogger(),
				InstanceKey:           "test-instance",
				SystemID:              "test-system",
				Registry:              registry,
				DisableQueryRedaction: true,
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
		Receiver:              loki.NewLogsReceiver(),
		EntryHandler:          entryHandler,
		Logger:                log.NewNopLogger(),
		InstanceKey:           "test",
		SystemID:              "test",
		Registry:              prometheus.NewRegistry(),
		DisableQueryRedaction: true,
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

func TestErrorLogsCollector_InvalidLogFormat(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 10), func() {})
	registry := prometheus.NewRegistry()

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:              loki.NewLogsReceiver(),
		EntryHandler:          entryHandler,
		Logger:                log.NewNopLogger(),
		InstanceKey:           "test",
		SystemID:              "test",
		Registry:              registry,
		DisableQueryRedaction: true,
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

// TestErrorLogsCollector_RDSLikeLogs tests with comprehensive RDS-like log samples
func TestErrorLogsCollector_RDSLikeLogs(t *testing.T) {
	entryHandler := loki.NewEntryHandler(make(chan loki.Entry, 100), func() {})
	registry := prometheus.NewRegistry()

	collector, err := NewErrorLogs(ErrorLogsArguments{
		Receiver:              loki.NewLogsReceiver(),
		EntryHandler:          entryHandler,
		Logger:                log.NewNopLogger(),
		InstanceKey:           "rds-test-instance",
		SystemID:              "rds-system",
		Registry:              registry,
		DisableQueryRedaction: true,
	})
	require.NoError(t, err)

	err = collector.Start(context.Background())
	require.NoError(t, err)
	defer collector.Stop()

	// Real RDS-like log samples covering various error types
	logSamples := []struct {
		log              string
		expectedSQLState string
		expectedCategory string
	}{
		{
			log:              `2025-01-12 10:30:45.123 UTC|app-user|books_store|10.0.1.5:54321|9112|4|57014|2025-01-12 10:29:15 UTC|25/112|0|693c34cb.2398|SELECT|0|psql|5457019535816659310|ERROR:  canceling statement due to statement timeout`,
			expectedSQLState: "57014",
			expectedCategory: "Operator Intervention",
		},
		{
			log:              `2025-01-12 10:31:23.258 UTC|app-user|books_store|10.0.1.5:54321|9185|9|40P01|2025-01-12 10:29:19 UTC|36/148|837|693c34cf.23e1|UPDATE|0|webapp|3188095831510673590|ERROR:  deadlock detected`,
			expectedSQLState: "40P01",
			expectedCategory: "Transaction Rollback",
		},
		{
			log:              `2025-01-12 10:32:31.529 UTC|conn_limited|books_store|10.0.1.10:45678|9449|4|53300|2025-01-12 10:32:31 UTC|91/57|0|693c34db.24e9|startup|0|api_worker|6883023751393440299|FATAL:  too many connections for role "conn_limited"`,
			expectedSQLState: "53300",
			expectedCategory: "Insufficient Resources",
		},
		{
			log:              `2025-01-12 10:33:42.201 UTC|app-user|books_store|10.0.1.20:52860|9589|2|28P01|2025-01-12 10:33:42 UTC|159/363|0|693c34e6.2575|authentication|0|jdbc_client|225649433808025698|FATAL:  password authentication failed for user "app-user"`,
			expectedSQLState: "28P01",
			expectedCategory: "Invalid Authorization Specification",
		},
		{
			log:              `2025-01-12 10:34:15.891 UTC|web-user|orders_db|10.0.1.5:54322|10123|7|23505|2025-01-12 10:30:00 UTC|42/201|1045|693c3500.2790|INSERT|0|web_app|8123456789012345678|ERROR:  duplicate key value violates unique constraint "orders_pkey"`,
			expectedSQLState: "23505",
			expectedCategory: "Integrity Constraint Violation",
		},
		{
			log:              `2025-01-12 10:35:22.456 UTC|api-user|products_db|10.0.1.8:43210|10456|5|23503|2025-01-12 10:35:00 UTC|55/89|1123|693c3512.28d8|INSERT|0|rest_api|9876543210987654321|ERROR:  insert or update on table "order_items" violates foreign key constraint "order_items_product_id_fkey"`,
			expectedSQLState: "23503",
			expectedCategory: "Integrity Constraint Violation",
		},
		{
			log:              `2025-01-12 10:36:10.789 UTC|analytics-user|reports_db|10.0.1.15:54323|11234|3|22012|2025-01-12 10:35:45 UTC|67/145|0|693c3520.2be2|SELECT|0|analytics_tool|1234567890123456789|ERROR:  division by zero`,
			expectedSQLState: "22012",
			expectedCategory: "Data Exception",
		},
		{
			log:              `2025-01-12 10:42:50.789 UTC|app-user|app_db|10.0.1.50:54329|13012|4|42P01|2025-01-12 10:40:00 UTC|134/789|2001|693c3580.32d4|SELECT|0|app_server|2222222222222222222|ERROR:  relation "non_existent_table" does not exist`,
			expectedSQLState: "42P01",
			expectedCategory: "Syntax Error or Access Rule Violation",
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
		if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
			errorMetrics = mf
			break
		}
	}

	require.NotNil(t, errorMetrics, "error metrics should exist")
	require.GreaterOrEqual(t, len(errorMetrics.GetMetric()), len(logSamples), "should have metrics for all error types")

	// Verify specific SQLSTATE codes and categories were captured
	capturedStates := make(map[string]string)
	for _, metric := range errorMetrics.GetMetric() {
		labels := make(map[string]string)
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		capturedStates[labels["sqlstate"]] = labels["error_category"]

		// Verify instance label is set correctly
		require.Equal(t, "rds-test-instance", labels["instance"])
	}

	// Verify all expected SQLSTATE codes were captured
	for _, sample := range logSamples {
		category, found := capturedStates[sample.expectedSQLState]
		require.True(t, found, "SQLSTATE %s should be captured", sample.expectedSQLState)
		require.Equal(t, sample.expectedCategory, category, "Category for %s should match", sample.expectedSQLState)
	}

	// Verify logs without SQLSTATE or with INFO/LOG severity were skipped
	collector.Receiver().Chan() <- loki.Entry{
		Entry: push.Entry{
			Line:      `2025-01-12 10:45:00.000 UTC|app-user|books_store|10.0.1.5:54321|9112|5||2025-01-12 10:29:15 UTC|25/112|0|693c34cb.2398|idle|0|psql|0|LOG:  connection received`,
			Timestamp: time.Now(),
		},
	}

	time.Sleep(100 * time.Millisecond)

	// Metric count should not have increased
	mfsAfter, _ := registry.Gather()
	for _, mf := range mfsAfter {
		if mf.GetName() == "postgres_errors_by_sqlstate_query_user_total" {
			require.Equal(t, len(errorMetrics.GetMetric()), len(mf.GetMetric()), "should not create metrics for non-error logs")
		}
	}
}
