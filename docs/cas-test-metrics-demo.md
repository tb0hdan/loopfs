# cas-test Metrics Feature Demo

The `cas-test` tool now includes comprehensive metrics tracking for each operation with a toggleable summary feature.

## Features Added

### 1. Operation-Level Metrics
- **Upload**: Tracks duration and bytes uploaded
- **Download**: Tracks duration and bytes downloaded
- **Info**: Tracks duration of metadata fetches
- **Delete**: Tracks duration of delete operations

### 2. Step-Level Metrics
Each test step tracks:
- Total duration
- Number of operations per type
- Average operation duration
- Success/failure status
- Error details if any

### 3. Summary Report (Enabled by Default)
The summary includes:
- Overall statistics (total uploads, downloads, info, deletes, bytes)
- Step-by-step breakdown with operation counts and average durations
- Total execution time
- Average throughput calculation

## Usage

### Basic Usage (Summary Enabled)
```bash
./build/cas-test -step1
```

### Disable Summary
```bash
./build/cas-test -step1 -no-summary
```

### Full Test with Summary
```bash
./build/cas-test -all
```

## Sample Output

When the server is running and tests complete successfully, you'll see:

```
Step 1: Running single pass
✓ Step 1 completed successfully

Step 2: Running 10 sequential passes
  Pass 1/10...
  Pass 2/10...
  ...
✓ Step 2 completed successfully

✅ All selected test scenarios completed successfully

================================================================================
METRICS SUMMARY
================================================================================

Overall Statistics:
  Total uploads:   11
  Total downloads: 11
  Total info:      11
  Total deletes:   11
  Total bytes:     22.5 KB

Step-by-Step Breakdown:

  ✓ Step 1: Single pass (0.30s)
    - upload: 1 operations, avg 150.000ms
    - info: 1 operations, avg 20.000ms
    - download: 1 operations, avg 100.000ms
    - delete: 1 operations, avg 30.000ms

  ✓ Step 2: 10 sequential passes (2.78s)
    - upload: 10 operations, avg 140.000ms
    - info: 10 operations, avg 18.000ms
    - download: 10 operations, avg 95.000ms
    - delete: 10 operations, avg 25.000ms

Timing Summary:
  Total execution time: 3.08s
  Average throughput:   7.3 KB/s
================================================================================
```

## Implementation Details

The metrics are collected using:
- `metricsCollector` struct for tracking all metrics
- Thread-safe operations with mutex protection
- Per-operation timing with `time.Since()` measurements
- Automatic bytes tracking for upload/download operations
- Human-readable byte formatting (B, KB, MB, etc.)

## Command-Line Flag

- `-no-summary`: Disable the metrics summary (summary is enabled by default)

This feature helps in:
1. Performance analysis of the CAS server
2. Identifying bottlenecks in specific operations
3. Monitoring throughput and latency
4. Debugging issues with detailed operation-level timing