# PostgreSQL Error Logs: RDS Stderr Text Format Support

## 🎯 Quick Summary

**Decision:** Implement stderr text format parser (no JSON support)

**Why:** AWS RDS doesn't support `jsonlog` format, only `stderr` and `csvlog`

**Solution:** Use PostgreSQL 14+ with `compute_query_id = on` and `%Q` in log_line_prefix to capture query_id

**Phase 1 (Current):** Metrics only - extract 5 key fields for rich error metrics by instance, database, user, query_id, SQLSTATE, error class/category

**Result:** Query_id correlation with pg_stat_statements + comprehensive error categorization! ✅

### Required Configuration

```sql
-- PostgreSQL 14+ settings
compute_query_id = 'on'
log_destination = 'stderr'
log_line_prefix = '%m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|'
log_error_verbosity = 'default'
log_min_error_statement = 'error'
```

---

## Problem Statement

The current implementation in branch `gaantunes/pg-logs-error-grok` parses PostgreSQL error logs in **JSON format** (`log_destination = 'jsonlog'`). However, AWS RDS PostgreSQL **does not support** the `jsonlog` format.

RDS only supports:
- **stderr** (default text format) ✅ 
- **csvlog** (CSV format) ✅

To make this work with RDS, we need to support parsing the **stderr text format** with a comprehensive `log_line_prefix` configuration.

## ✅ SOLUTION: Stderr Text Format with %Q for query_id

**Decision: Focus solely on stderr text format parsing.** No JSON support needed.

PostgreSQL 14+ supports `%Q` in `log_line_prefix`, which outputs the `query_id` when `compute_query_id = on`. This means we can capture **ALL critical fields** including `query_id` using text format (stderr)!

### Key Requirements:
- PostgreSQL 14 or higher ✅
- `compute_query_id = on` ✅
- `log_line_prefix` includes `%Q` ✅
- `log_destination = 'stderr'` ✅
- RDS fully supports this configuration ✅

### Trade-offs:
- ❌ Lose `backend_type` field (not available in log_line_prefix)
- ✅ Keep ALL other critical fields including `query_id`
- ✅ Simpler implementation (single parser)
- ✅ Works perfectly with RDS

---

## Current JSON Log Fields Captured

Based on the `PostgreSQLJSONLog` struct in `error_logs.go`, here are all the fields currently being extracted:

### Core Identification
- `timestamp` - Timestamp with timezone
- `pid` - Process ID
- `session_id` - Unique session identifier
- `line_num` - Line number for this session

### User/Database Context
- `user` - Database user
- `dbname` - Database name

### Client Information
- `remote_host` - Client hostname/IP
- `remote_port` - Client port
- `application_name` - Application name

### Session/Process Info
- `ps` - Current ps display (command status)
- `session_start` - Session start time
- `backend_type` - Type of backend process

### Transaction Information
- `vxid` - Virtual transaction ID (e.g., "3/1234")
- `txid` - Transaction ID

### Error/Log Information
- `error_severity` - ERROR, FATAL, PANIC, etc.
- `state_code` - SQLSTATE code (e.g., "57014")
- `message` - Error message
- `detail` - Detailed error description
- `hint` - Error hint
- `context` - Error context

### Query Information
- `statement` - The SQL statement that caused the error
- `cursor_position` - Position in statement where error occurred
- `query_id` - Query ID (requires pg_stat_statements with compute_query_id)

### Internal Query (for errors in functions/procedures)
- `internal_query` - Internal SQL query
- `internal_position` - Position in internal query

### Error Location (for PostgreSQL internal errors)
- `func_name` - Function name where error occurred
- `file_name` - Source file name
- `file_line_num` - Line number in source file

### Parallel Query Support
- `leader_pid` - PID of leader for active parallel workers

---

## PostgreSQL log_line_prefix Options

Here are the available `log_line_prefix` escape sequences:

| Escape | Description | JSON Equivalent | RDS Support |
|--------|-------------|-----------------|-------------|
| `%a` | Application name | `application_name` | ✅ Yes |
| `%u` | User name | `user` | ✅ Yes |
| `%d` | Database name | `dbname` | ✅ Yes |
| `%r` | Remote host (with port) | `remote_host`+`remote_port` | ✅ Yes |
| `%h` | Remote host only | `remote_host` | ✅ Yes |
| `%p` | Process ID | `pid` | ✅ Yes |
| `%P` | Process ID of parallel group leader | `leader_pid` | ✅ Yes (PG 13+) |
| `%t` | Timestamp (no milliseconds) | `timestamp` | ✅ Yes |
| `%m` | Timestamp with milliseconds | `timestamp` | ✅ Yes |
| `%n` | Timestamp (Unix epoch) | `timestamp` | ✅ Yes |
| `%i` | Command tag | `ps` | ✅ Yes |
| `%e` | SQLSTATE error code | `state_code` | ✅ Yes |
| `%c` | Session ID | `session_id` | ✅ Yes |
| `%l` | Session line number | `line_num` | ✅ Yes |
| `%s` | Process start timestamp | `session_start` | ✅ Yes |
| `%v` | Virtual transaction ID | `vxid` | ✅ Yes |
| `%x` | Transaction ID | `txid` | ✅ Yes |
| `%Q` | Query ID | `query_id` | ✅ Yes (PG 14+) |
| `%q` | Produces no output (stop output) | N/A | ✅ Yes |
| `%%` | Literal % | N/A | ✅ Yes |

---

## Recommended log_line_prefix for Maximum Coverage

```
%m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|
```

### Format explanation:
- **Delimiter**: Using `|` (pipe) as field separator (easy to parse, unlikely in data)
- **Order**: Fixed order makes parsing predictable
- **Requirements**: PostgreSQL 14+ for `%Q` (query_id), `compute_query_id = on`
- **Fields included**:
  1. `%m` - Timestamp with milliseconds
  2. `%u` - User name
  3. `%d` - Database name  
  4. `%r` - Remote host:port
  5. `%p` - Process ID
  6. `%l` - Line number
  7. `%e` - SQLSTATE
  8. `%s` - Session start
  9. `%v` - Virtual transaction ID
  10. `%x` - Transaction ID
  11. `%c` - Session ID
  12. `%i` - Command tag (ps)
  13. `%P` - Parallel leader PID
  14. `%a` - Application name
  15. `%Q` - Query ID (PG 14+, requires compute_query_id = on)

### Example output:
```
2025-01-12 10:30:45.123 UTC|app-user|books_store|10.0.1.5:54321|9112|4|57014|2025-01-12 10:29:15 UTC|25/112|837|693c34cb.2398|SELECT|0|psql|5457019535816659310|ERROR:  canceling statement due to statement timeout
DETAIL:  ...
STATEMENT:  SELECT pg_sleep(5);
```

---

## Fields NOT Available in Text Format

Some fields from JSON logs are **NOT available** in `log_line_prefix`:

❌ **backend_type** - Not available via log_line_prefix
- **Workaround**: Can sometimes be inferred from context or process type
- **Impact**: Low - mostly informational

✅ **query_id** - Available via `%Q` in PostgreSQL 14+
- **Requirements**: PostgreSQL 14+, `compute_query_id = on`, include `%Q` in log_line_prefix
- **Impact**: HIGH - Critical for correlation with pg_stat_statements
- **Status**: FULLY SUPPORTED in text format! 🎉

❌ **cursor_position** - Not in prefix, but appears in error context
- **Workaround**: Parse from error message "at character N"
- **Impact**: Medium - useful for pinpointing error location

❌ **internal_query** / **internal_position** - Logged separately in CONTEXT
- **Workaround**: Parse from CONTEXT lines
- **Impact**: Low - only for PL/pgSQL errors

❌ **func_name**, **file_name**, **file_line_num** - PostgreSQL internals
- **Workaround**: Parse from LOCATION lines
- **Impact**: Very Low - mostly for PostgreSQL developers

---

## Additional RDS Configuration Requirements

### Essential Configuration for Full Observability:

```sql
-- REQUIRED: Enable query_id computation (PG 14+)
compute_query_id = 'on'

-- REQUIRED: Set comprehensive log_line_prefix with query_id
log_line_prefix = '%m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|'

-- REQUIRED: Log error details
log_error_verbosity = 'default'  -- or 'verbose' for more info

-- REQUIRED: Log statements that cause errors
log_min_error_statement = 'error'  -- Log statements causing errors

-- OPTIONAL: Log all statements (verbose, use with caution in production)
-- log_statement = 'all'

-- REQUIRED: Use stderr as log destination
log_destination = 'stderr'
```

### RDS Parameter Group Settings:

For AWS RDS PostgreSQL 14+, modify your parameter group:

1. **compute_query_id** = `on`
2. **log_line_prefix** = `%m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|`
3. **log_error_verbosity** = `default` (or `verbose`)
4. **log_min_error_statement** = `error`
5. **log_destination** = `stderr`

---

## Implementation Architecture

### Single Text Parser (Stderr Only) ⭐

**Decision: Implement a single, focused text parser for stderr logs.**

```go
func (c *ErrorLogs) processLogLine(entry loki.Entry) error {
    return c.parseTextLog(entry)
}

func (c *ErrorLogs) parseTextLog(entry loki.Entry) error {
    // Parse log_line_prefix with %Q for query_id
    // Expected format: %m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|
    
    line := entry.Entry.Line
    
    // Split on first 15 pipes to extract prefix fields
    parts := strings.SplitN(line, "|", 16)
    
    if len(parts) < 16 {
        c.parseErrors.Inc()
        return fmt.Errorf("invalid log line format: expected 15 prefix fields")
    }
    
    parsed := &ParsedError{}
    
    // Field 0: %m - Timestamp with milliseconds
    timestamp, err := parseTimestamp(parts[0])
    if err != nil {
        return fmt.Errorf("failed to parse timestamp: %w", err)
    }
    parsed.Timestamp = timestamp
    
    // Field 1: %u - User name
    parsed.User = strings.TrimSpace(parts[1])
    
    // Field 2: %d - Database name
    parsed.DatabaseName = strings.TrimSpace(parts[2])
    
    // Field 3: %r - Remote host:port
    parsed.RemoteHost, parsed.RemotePort = parseRemoteAddress(parts[3])
    
    // Field 4: %p - Process ID
    parsed.PID = parseInt32(parts[4])
    
    // Field 5: %l - Line number
    parsed.LineNum = parseInt32(parts[5])
    
    // Field 6: %e - SQLSTATE error code
    parsed.SQLState = strings.TrimSpace(parts[6])
    if parsed.SQLState != "" {
        parsed.ErrorName = GetSQLStateErrorName(parsed.SQLState)
        parsed.SQLStateClass = parsed.SQLState[:2]
        parsed.ErrorCategory = GetSQLStateCategory(parsed.SQLState)
    }
    
    // Field 7: %s - Session start timestamp
    parsed.SessionStart, _ = parseTimestamp(parts[7])
    
    // Field 8: %v - Virtual transaction ID
    parsed.VXID = strings.TrimSpace(parts[8])
    
    // Field 9: %x - Transaction ID
    parsed.TXID = strings.TrimSpace(parts[9])
    
    // Field 10: %c - Session ID
    parsed.SessionID = strings.TrimSpace(parts[10])
    
    // Field 11: %i - Command tag (ps)
    parsed.PS = strings.TrimSpace(parts[11])
    
    // Field 12: %P - Parallel leader PID
    parsed.LeaderPID = parseInt32(parts[12])
    
    // Field 13: %a - Application name
    parsed.ApplicationName = strings.TrimSpace(parts[13])
    
    // Field 14: %Q - Query ID ⭐ THE CRITICAL FIELD!
    parsed.QueryID = parseInt64(parts[14])
    
    // Field 15: Remaining part is the actual log message
    messageAndRest := parts[15]
    
    // Parse severity and message from the remaining content
    // Format: "ERROR:  message text"
    parsed.ErrorSeverity, parsed.Message = parseSeverityAndMessage(messageAndRest)
    
    // Check if this is a severity we care about
    if !supportedSeverities[parsed.ErrorSeverity] {
        return nil // Skip this log entry
    }
    
    // Handle multi-line messages (DETAIL, HINT, CONTEXT, STATEMENT)
    // These will come in subsequent log lines without the prefix
    // We need to buffer and collect them (state machine approach)
    
    return nil
}
```

**Pros:**
- ✅ **Simple, focused implementation** - Single parser to maintain
- ✅ **Captures query_id** via `%Q` (PostgreSQL 14+)
- ✅ **Predictable format** - Fixed 15-field pipe-delimited prefix
- ✅ **RDS compatible** - Works perfectly with AWS RDS
- ✅ **High performance** - Simple string splitting, no regex overhead
- ✅ **Clear requirements** - Users know exactly what to configure

**Cons:**
- ❌ Requires exact `log_line_prefix` configuration
- ❌ No backward compatibility with JSON logs
- ⚠️ Multi-line parsing requires careful state management

**Rationale:**
- All target databases will be PostgreSQL 14+ with `compute_query_id = on`
- Consistent configuration is desired across all monitored databases
- Simpler code is easier to maintain and test
- Performance is better with simple parsing vs regex/grok patterns

---
## Text Log Parsing Challenges

### 1. Multi-line Messages
Error logs span multiple lines:
```
2025-01-12 10:30:45.123 UTC|...|ERROR:  canceling statement due to statement timeout
DETAIL:  Process 9185 waits for ShareLock on transaction 836
HINT:  See server log for query details.
CONTEXT:  while locking tuple (3,88) in relation "books"
STATEMENT:  UPDATE books SET stock = stock WHERE id = 2;
```

**Solution**: State machine parser that collects lines until next log entry

### 2. Inconsistent Field Presence
Not all fields appear in every log line:
- Transaction ID is 0 for non-transactional commands
- SQLSTATE only for errors
- Remote host may be `[local]` for Unix socket connections

**Solution**: Optional field handling with null/zero checks

### 3. Timestamp Parsing
Multiple timestamp formats depending on locale and configuration:
- `2025-01-12 10:30:45.123 UTC`
- `2025-01-12 10:30:45.123 EST`
- Different date formats based on locale

**Solution**: Try multiple timestamp formats, configurable timezone

### 4. Delimiter Conflicts
Chosen delimiter might appear in data (though unlikely with `|`):

**Solution**: 
- Use `|` as delimiter (rare in data)
- Parse fixed number of fields from prefix
- Everything after last delimiter is message content

---

## Compatibility Matrix

| Feature | JSON Log | Text Log (stderr) | CSV Log |
|---------|----------|-------------------|---------|
| All core fields | ✅ | ✅ | ✅ |
| query_id | ✅ | ✅ (PG 14+ with %Q) | ✅ |
| backend_type | ✅ | ❌ | ✅ |
| Structured parsing | ✅ Easy | ⚠️ Moderate | ✅ Easy |
| RDS Support | ❌ | ✅ | ✅ |
| Multi-line handling | ✅ | ⚠️ Complex | ✅ |

---

## Next Steps - Implementation Plan

### ✅ Decision Made: Stderr Text Parser Only

**Focus solely on stderr text format** with PostgreSQL 14+ and `compute_query_id = on` for full `query_id` support.

### Phase 1: Metrics-Only Parser (Simplified First Step)

**Goal:** Parse just enough to produce metrics - no full log emission to Loki yet.

**Fields needed for metrics:**
- Instance (already available in collector)
- Database name (field 3: `%d`)
- User (field 2: `%u`) 
- Query ID (field 15: `%Q`) ⭐
- SQLSTATE (field 7: `%e`)
- Error severity (from message part)

1. **Simplify `error_logs.go` for Phase 1**
   - Keep `ParsedError` struct but only populate fields needed for metrics
   - Remove complex multi-line parsing (not needed for metrics)
   - Focus on parsing the prefix fields only

2. **Implement minimal `parseTextLog()` function**
   
   Here's the complete Phase 1 implementation:
   
   ```go
   // parseTextLog extracts only the fields needed for metrics in Phase 1
   func (c *ErrorLogs) parseTextLog(entry loki.Entry) error {
       line := entry.Entry.Line
       
       // Expected format: %m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|MESSAGE
       // Split into 16 parts: 15 prefix fields + message
       parts := strings.SplitN(line, "|", 16)
       if len(parts) < 16 {
           c.parseErrors.Inc()
           return fmt.Errorf("invalid log line format: expected 16 pipe-delimited fields, got %d", len(parts))
       }
       
       // Extract ONLY the 5 fields needed for metrics
       user := strings.TrimSpace(parts[1])           // Field 2: %u (user)
       database := strings.TrimSpace(parts[2])       // Field 3: %d (database)
       sqlstate := strings.TrimSpace(parts[6])       // Field 7: %e (SQLSTATE)
       queryIDStr := strings.TrimSpace(parts[14])    // Field 15: %Q (query_id) ⭐
       messageAndRest := parts[15]                   // Field 16: severity + message
       
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
       errorName := GetSQLStateErrorName(sqlstate)      // e.g., "query_canceled"
       sqlstateClass := sqlstate[:2]                     // e.g., "57"
       errorCategory := GetSQLStateCategory(sqlstate)   // e.g., "Operator Intervention"
       
       // Convert queryID to string for metric label
       queryIDLabel := ""
       if queryID > 0 {
           queryIDLabel = strconv.FormatInt(queryID, 10)
       }
       
       // Emit metrics (existing function)
       c.errorsBySQLState.WithLabelValues(
           sqlstate,        // sqlstate: "57014"
           errorName,       // error_name: "query_canceled"
           sqlstateClass,   // sqlstate_class: "57"
           errorCategory,   // error_category: "Operator Intervention"
           severity,        // severity: "ERROR"
           database,        // database: "books_store"
           user,            // user: "app-user"
           queryIDLabel,    // queryid: "5457019535816659310"
           c.instanceKey,   // instance: "orders_db"
       ).Inc()
       
       return nil
   }
   
   // extractSeverity parses the severity from the message part
   // Input: "ERROR:  canceling statement due to timeout"
   // Output: "ERROR"
   func extractSeverity(message string) string {
       // Format is typically "SEVERITY:  message text"
       if idx := strings.Index(message, ":"); idx > 0 {
           return strings.TrimSpace(message[:idx])
       }
       return ""
   }
   ```

3. **Use existing SQLSTATE maps**
   - Already have `SQLStateErrors` map in `error_logs_sqlstate.go` ✅
   - Already have `SQLStateClass` map in `error_logs_sqlstate.go` ✅
   - Already have helper functions:
     - `GetSQLStateErrorName(sqlstate)` ✅
     - `GetSQLStateCategory(sqlstate)` ✅

4. **Keep existing metrics**
   - `postgres_errors_by_sqlstate_query_user_total` with labels:
     - `sqlstate` - The 5-character SQLSTATE code
     - `error_name` - Human-readable error name
     - `sqlstate_class` - First 2 characters of SQLSTATE
     - `error_category` - Category from SQLStateClass map
     - `severity` - ERROR, FATAL, or PANIC
     - `database` - Database name
     - `user` - User name
     - `queryid` - Query ID for correlation
     - `instance` - Instance identifier

### Phase 2: Full Log Parsing to Loki (Future Enhancement)

**Goal:** Parse complete error logs and emit to Loki with all fields.

This phase adds:
- Full timestamp parsing
- All prefix fields (PID, session_id, vxid, txid, etc.)
- Multi-line error parsing (DETAIL, HINT, CONTEXT, STATEMENT)
- Query redaction for PII protection
- Complete Loki log emission

**Deferred until Phase 1 metrics are validated.**

---

### Phase 3: Comprehensive Testing

1. **Unit tests for text parsing**
   - Test all 15 prefix fields
   - Test query_id extraction
   - Test various error severities (ERROR, FATAL, PANIC)
   - Test edge cases:
     - Missing/empty fields
     - Local connections ([local])
     - Zero transaction IDs
     - Parallel queries (leader PID)
     - Different timestamp formats/timezones

2. **Multi-line error tests**
   - Deadlock with DETAIL/HINT/CONTEXT
   - Statement timeout with STATEMENT
   - Constraint violation with DETAIL
   - Multiple errors interleaved from different PIDs

3. **Integration tests**
   - Generate RDS-like log samples
   - Verify metrics are created correctly
   - Verify Loki output format
   - Test query redaction in STATEMENT/DETAIL/CONTEXT

### Phase 3: Documentation

1. **Update component documentation**
   - **REQUIRED** RDS parameter group settings
   - Exact `log_line_prefix` format to use
   - PostgreSQL 14+ requirement
   - `compute_query_id = on` requirement
   - Example Alloy configuration

2. **Add troubleshooting guide**
   - What to do if logs aren't parsing
   - How to verify log_line_prefix is correct
   - How to check compute_query_id is enabled

### Phase 4: Validation with Real Data

1. **Test with RDS PostgreSQL 14+**
2. **Verify query_id correlation** with pg_stat_statements
3. **Load testing** with high-volume error logs
4. **Verify all SQLSTATE categories** are captured

### Implementation Checklist

**Phase 1: Metrics Only** (High Priority)

Code Changes:
- [ ] Simplify `error_logs.go` - remove JSON parsing
- [ ] Implement minimal `parseTextLog()` for metrics
- [ ] Extract 5 key fields: user, database, sqlstate, queryid, severity
- [ ] Use existing `GetSQLStateErrorName()` and `GetSQLStateCategory()`
- [ ] Keep existing `updateMetrics()` function
- [ ] Remove Loki log emission temporarily

Testing:
- [ ] Unit tests for text log line parsing
- [ ] Test query_id extraction from field 15 (`%Q`)
- [ ] Test SQLSTATE extraction and mapping
- [ ] Test all supported severities (ERROR, FATAL, PANIC)
- [ ] Test with missing/empty sqlstate
- [ ] Test with missing/zero query_id

Documentation:
- [ ] Document required log_line_prefix format
- [ ] Document PostgreSQL 14+ requirement
- [ ] Document RDS parameter group settings
- [ ] Add metric labels documentation

Validation:
- [ ] Test with real RDS logs
- [ ] Verify metrics are created correctly
- [ ] Verify query_id correlation with pg_stat_statements
- [ ] Verify SQLSTATE categorization

---

**Phase 2: Full Loki Logs** (Future)

Code Changes:
- [ ] Parse all 15 prefix fields
- [ ] Implement multi-line parsing (DETAIL, HINT, CONTEXT, STATEMENT)
- [ ] Add query redaction for PII protection
- [ ] Re-implement Loki log emission
- [ ] Add timestamp parsing

Testing:
- [ ] Multi-line error tests
- [ ] Query redaction tests
- [ ] Full integration tests

---

**Current Focus:** Phase 1 only - get metrics working first! 🎯

---

## Summary

**Decision: Single stderr text parser with fixed log_line_prefix format.**

### Phase 1: Metrics Only (Current Focus) 🎯

**Goal:** Parse just enough to produce comprehensive error metrics by:
- Instance
- Database
- User
- Query ID
- SQLSTATE code
- Error class (first 2 chars of SQLSTATE)
- Error category (from existing `SQLStateClass` map)
- Severity (ERROR, FATAL, PANIC)

**Implementation:**
- Simple 5-field extraction from pipe-delimited log_line_prefix
- Use existing `error_logs_sqlstate.go` maps for categorization
- Emit metrics via existing `updateMetrics()` function
- **No Loki log emission yet** - metrics first!

**What We Get in Phase 1:**
✅ Critical metric dimensions including **query_id**  
✅ SQLSTATE error categorization  
✅ Correlation with pg_stat_statements via query_id  
✅ RDS compatibility  
✅ Simple, focused implementation  
✅ High performance  

**Deferred to Phase 2:**
- Full log parsing (all 15 fields)
- Multi-line error handling (DETAIL, HINT, CONTEXT, STATEMENT)
- Loki log emission
- Query redaction

### Requirements:
- PostgreSQL 14 or higher
- `compute_query_id = on`
- `log_destination = 'stderr'`
- Exact `log_line_prefix` format: `%m|%u|%d|%r|%p|%l|%e|%s|%v|%x|%c|%i|%P|%a|%Q|`

This phased approach gets you **actionable metrics quickly** while keeping the door open for full log parsing later! 🚀

---

## Parsing Example (Phase 1: Metrics Only)

### Input: Text Log Line (stderr)
```
2025-12-12 15:29:16.068 GMT|app-user|books_store|[local]|9112|4|57014|2025-12-12 15:29:15 GMT|25/112|0|693c34cb.2398|SELECT|0|psql|5457019535816659310|ERROR:  canceling statement due to statement timeout
STATEMENT:  SELECT pg_sleep(5);
```

### Field Extraction (Phase 1)
```
Field  1: %m  = "2025-12-12 15:29:16.068 GMT"  (skipped in Phase 1)
Field  2: %u  = "app-user"                      ✅ Extract for metrics
Field  3: %d  = "books_store"                   ✅ Extract for metrics
Field  4: %r  = "[local]"                       (skipped in Phase 1)
Field  5: %p  = "9112"                          (skipped in Phase 1)
Field  6: %l  = "4"                             (skipped in Phase 1)
Field  7: %e  = "57014"                         ✅ Extract for metrics (SQLSTATE)
Field  8: %s  = "2025-12-12 15:29:15 GMT"      (skipped in Phase 1)
Field  9: %v  = "25/112"                        (skipped in Phase 1)
Field 10: %x  = "0"                             (skipped in Phase 1)
Field 11: %c  = "693c34cb.2398"                 (skipped in Phase 1)
Field 12: %i  = "SELECT"                        (skipped in Phase 1)
Field 13: %P  = "0"                             (skipped in Phase 1)
Field 14: %a  = "psql"                          (skipped in Phase 1)
Field 15: %Q  = "5457019535816659310"           ✅ Extract for metrics (query_id) ⭐
Field 16: msg = "ERROR:  canceling..."          ✅ Extract severity
```

### Minimal Parsed Data (Phase 1)
```go
// Only populate fields needed for metrics
ParsedError{
    User:          "app-user",
    DatabaseName:  "books_store",
    SQLState:      "57014",
    ErrorName:     "query_canceled",           // From GetSQLStateErrorName()
    SQLStateClass: "57",                       // First 2 chars of SQLSTATE
    ErrorCategory: "Operator Intervention",    // From GetSQLStateCategory()
    ErrorSeverity: "ERROR",                    // Parsed from message
    QueryID:       5457019535816659310,        // ⭐ From %Q
}
```

### Prometheus Metrics (Phase 1 Output) ⭐
```prometheus
postgres_errors_by_sqlstate_query_user_total{
    sqlstate="57014",
    error_name="query_canceled",
    sqlstate_class="57",
    error_category="Operator Intervention",
    severity="ERROR",
    database="books_store",
    user="app-user",
    queryid="5457019535816659310",  # ⭐ Can correlate with pg_stat_statements!
    instance="orders_db"
} 1
```

### Key Observations (Phase 1)
1. ✅ **query_id is captured** - Full correlation with pg_stat_statements
2. ✅ **SQLSTATE categorization** - Using existing maps from `error_logs_sqlstate.go`
3. ✅ **Error class and category** - Rich dimensional data for alerting
4. ✅ **Per-database, per-user tracking** - Granular error attribution
5. ✅ **Simple implementation** - Only 5 fields extracted, no complex multi-line parsing
6. 🔮 **Phase 2 will add** - Full Loki logs with all fields, multi-line details, query redaction

