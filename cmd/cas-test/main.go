package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultServerURL        = "http://127.0.0.1:8080"
	defaultFileSize         = 1024
	defaultMultiPassCount   = 10
	defaultParallelInfo     = 10
	defaultFullPassParallel = 10
	defaultHTTPTimeout      = 2 * time.Minute

	separatorLineLength     = 80
	microsecondsToMillis    = 1000.0
)

type config struct {
	serverURL          string
	fileSize           int
	multiPassCount     int
	parallelInfoCount  int
	fullPassConcurrent int
	httpTimeout        time.Duration

	// Test selection flags
	runAll              bool
	runSinglePass       bool
	runMultiPass        bool
	runParallelInfoSame bool
	runParallelInfoDiff bool
	runFullParallel     bool

	// Metrics configuration
	showSummary bool
}

type tester struct {
	cfg     config
	client  *casClient
	metrics *metricsCollector
}

type passResult struct {
	Hash string
}

// Operation metrics.
type operationMetrics struct {
	Name     string
	Duration time.Duration
	Size     int64
	Error    error
}

// Step metrics for a complete test step.
type stepMetrics struct {
	Name       string
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Operations []operationMetrics
	Success    bool
	Error      error
}

// Metrics collector for tracking all operations.
type metricsCollector struct {
	mu           sync.Mutex
	steps        []stepMetrics
	currentStep  *stepMetrics
	showSummary  bool
	totalUploads int
	totalDeletes int
	totalInfos   int
	totalDownloads int
	totalBytes   int64
}

type uploadResponse struct {
	Hash string `json:"hash"`
}

type fileInfoResponse struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// casClient provides a universal HTTP client for CAS operations.
type casClient struct {
	baseURL    string
	httpClient *http.Client
}

// newCASClient creates a new CAS client.
func newCASClient(baseURL string, timeout time.Duration) *casClient {
	return &casClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// doRequest performs an HTTP request with common error handling.
func (c *casClient) doRequest(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, fmt.Errorf("request returned %s: %s", resp.Status, string(respBody))
	}

	return respBody, nil
}

// doJSON performs a request and unmarshals JSON response.
func (c *casClient) doJSON(ctx context.Context, method, path string, body io.Reader, contentType string, result interface{}) error {
	respBody, err := c.doRequest(ctx, method, path, body, contentType)
	if err != nil {
		return err
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
	}

	return nil
}

func main() {
	cfg := parseFlags()
	tester := newTester(cfg)

	ctx := context.Background()
	if err := tester.run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cas-test failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ All selected test scenarios completed successfully")

	// Print metrics summary
	tester.metrics.printSummary()
}

func parseFlags() config {
	flags := parseCommandLineFlags()
	cfg := createConfigFromFlags(flags)
	validateAndNormalizeConfig(&cfg)
	return cfg
}

type parsedFlags struct {
	server       *string
	size         *int
	timeout      *time.Duration
	multi        *int
	parallelInfo *int
	fullParallel *int
	runAll       *bool
	step1        *bool
	step2        *bool
	step3        *bool
	step4        *bool
	step5        *bool
	noSummary    *bool
}

func parseCommandLineFlags() parsedFlags {
	// Server configuration
	server := flag.String("server", defaultServerURL, "CAS server base URL")
	size := flag.Int("size", defaultFileSize, "Test file size in bytes")
	timeout := flag.Duration("http-timeout", defaultHTTPTimeout, "HTTP client timeout")

	// Test counts
	multi := flag.Int("passes", defaultMultiPassCount, "Number of sequential passes to execute (for step 2)")
	parallelInfo := flag.Int("parallel-info", defaultParallelInfo, "Number of parallel /info requests to issue (for steps 3 & 4)")
	fullParallel := flag.Int("parallel-full", defaultFullPassParallel, "Number of concurrent full passes to execute (for step 5)")

	// Test selection flags
	runAll := flag.Bool("all", false, "Run all test steps (overrides individual step selections)")
	step1 := flag.Bool("step1", false, "Run Step 1: Single pass test")
	step2 := flag.Bool("step2", false, "Run Step 2: Multiple sequential passes")
	step3 := flag.Bool("step3", false, "Run Step 3: Parallel /info requests for same file")
	step4 := flag.Bool("step4", false, "Run Step 4: Parallel /info requests for different files")
	step5 := flag.Bool("step5", false, "Run Step 5: Full passes in parallel")

	// Metrics flags
	noSummary := flag.Bool("no-summary", false, "Disable metrics summary (summary is enabled by default)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nTest Steps:\n")
		fmt.Fprintf(os.Stderr, "  Step 1: Single pass (upload, info, download, delete)\n")
		fmt.Fprintf(os.Stderr, "  Step 2: Multiple sequential passes\n")
		fmt.Fprintf(os.Stderr, "  Step 3: Parallel /info requests for the same file\n")
		fmt.Fprintf(os.Stderr, "  Step 4: Parallel /info requests for different files\n")
		fmt.Fprintf(os.Stderr, "  Step 5: Full passes in parallel\n")
		fmt.Fprintf(os.Stderr, "\nBy default, all steps run. Use individual -step flags to run specific tests.\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	return parsedFlags{
		server:       server,
		size:         size,
		timeout:      timeout,
		multi:        multi,
		parallelInfo: parallelInfo,
		fullParallel: fullParallel,
		runAll:       runAll,
		step1:        step1,
		step2:        step2,
		step3:        step3,
		step4:        step4,
		step5:        step5,
		noSummary:    noSummary,
	}
}

func createConfigFromFlags(flags parsedFlags) config {
	// Check if any specific step was selected
	anyStepSelected := isAnyStepSelected(flags)

	cfg := createBaseConfig(flags, anyStepSelected)
	applyRunAllFlagOverride(&cfg, flags)

	return cfg
}

func isAnyStepSelected(flags parsedFlags) bool {
	return *flags.step1 || *flags.step2 || *flags.step3 || *flags.step4 || *flags.step5
}

func createBaseConfig(flags parsedFlags, anyStepSelected bool) config {
	return config{
		serverURL:          strings.TrimRight(*flags.server, "/"),
		fileSize:           *flags.size,
		multiPassCount:     *flags.multi,
		parallelInfoCount:  *flags.parallelInfo,
		fullPassConcurrent: *flags.fullParallel,
		httpTimeout:        *flags.timeout,

		// If no specific step is selected, run all
		runAll:              !anyStepSelected,
		runSinglePass:       *flags.step1 || !anyStepSelected,
		runMultiPass:        *flags.step2 || !anyStepSelected,
		runParallelInfoSame: *flags.step3 || !anyStepSelected,
		runParallelInfoDiff: *flags.step4 || !anyStepSelected,
		runFullParallel:     *flags.step5 || !anyStepSelected,

		// Metrics configuration - summary enabled by default
		showSummary: !*flags.noSummary,
	}
}

func applyRunAllFlagOverride(cfg *config, flags parsedFlags) {
	// Override with -all flag if explicitly set
	if *flags.runAll {
		cfg.runAll = true
		cfg.runSinglePass = true
		cfg.runMultiPass = true
		cfg.runParallelInfoSame = true
		cfg.runParallelInfoDiff = true
		cfg.runFullParallel = true
	}
}

func validateAndNormalizeConfig(cfg *config) {
	if cfg.serverURL == "" {
		cfg.serverURL = defaultServerURL
	}
	if cfg.fileSize <= 0 {
		fmt.Fprintf(os.Stderr, "invalid file size: %d\n", cfg.fileSize)
		os.Exit(1)
	}
	if cfg.multiPassCount <= 0 {
		cfg.multiPassCount = defaultMultiPassCount
	}
	if cfg.parallelInfoCount <= 0 {
		cfg.parallelInfoCount = defaultParallelInfo
	}
	if cfg.fullPassConcurrent <= 0 {
		cfg.fullPassConcurrent = defaultFullPassParallel
	}
}

func newTester(cfg config) *tester {
	client := newCASClient(cfg.serverURL, cfg.httpTimeout)
	metrics := &metricsCollector{
		showSummary: cfg.showSummary,
		steps:       make([]stepMetrics, 0),
	}
	return &tester{cfg: cfg, client: client, metrics: metrics}
}

// Metrics methods.
func (m *metricsCollector) startStep(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStep = &stepMetrics{
		Name:       name,
		StartTime:  time.Now(),
		Operations: make([]operationMetrics, 0),
	}
}

func (m *metricsCollector) endStep(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentStep != nil {
		m.currentStep.EndTime = time.Now()
		m.currentStep.Duration = m.currentStep.EndTime.Sub(m.currentStep.StartTime)
		m.currentStep.Success = err == nil
		m.currentStep.Error = err
		m.steps = append(m.steps, *m.currentStep)
		m.currentStep = nil
	}
}

func (m *metricsCollector) recordOperation(name string, duration time.Duration, size int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	operation := operationMetrics{
		Name:     name,
		Duration: duration,
		Size:     size,
		Error:    err,
	}

	if m.currentStep != nil {
		m.currentStep.Operations = append(m.currentStep.Operations, operation)
	}

	// Update totals
	switch name {
	case "upload":
		m.totalUploads++
		if size > 0 {
			m.totalBytes += size
		}
	case "download":
		m.totalDownloads++
		if size > 0 {
			m.totalBytes += size
		}
	case "info":
		m.totalInfos++
	case "delete":
		m.totalDeletes++
	}
}

func (m *metricsCollector) printSummary() {
	if !m.showSummary {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Println("\n" + strings.Repeat("=", separatorLineLength))
	fmt.Println("METRICS SUMMARY")
	fmt.Println(strings.Repeat("=", separatorLineLength))

	// Overall statistics
	fmt.Printf("\nOverall Statistics:\n")
	fmt.Printf("  Total uploads:   %d\n", m.totalUploads)
	fmt.Printf("  Total downloads: %d\n", m.totalDownloads)
	fmt.Printf("  Total info:      %d\n", m.totalInfos)
	fmt.Printf("  Total deletes:   %d\n", m.totalDeletes)
	fmt.Printf("  Total bytes:     %s\n", formatBytes(m.totalBytes))

	// Step-by-step breakdown
	fmt.Printf("\nStep-by-Step Breakdown:\n")
	for _, step := range m.steps {
		status := "✓"
		if !step.Success {
			status = "✗"
		}
		fmt.Printf("\n  %s %s (%.2fs)\n", status, step.Name, step.Duration.Seconds())

		// Operation counts per step
		opCounts := make(map[string]int)
		opDurations := make(map[string]time.Duration)
		for _, op := range step.Operations {
			opCounts[op.Name]++
			opDurations[op.Name] += op.Duration
		}

		for opName, count := range opCounts {
			avgDuration := opDurations[opName] / time.Duration(count)
			fmt.Printf("    - %s: %d operations, avg %.3fms\n", opName, count, float64(avgDuration.Microseconds())/microsecondsToMillis)
		}

		if step.Error != nil {
			fmt.Printf("    Error: %v\n", step.Error)
		}
	}

	// Timing statistics
	var totalDuration time.Duration
	for _, step := range m.steps {
		totalDuration += step.Duration
	}

	fmt.Printf("\nTiming Summary:\n")
	fmt.Printf("  Total execution time: %.2fs\n", totalDuration.Seconds())

	if m.totalUploads > 0 || m.totalDownloads > 0 {
		throughput := float64(m.totalBytes) / totalDuration.Seconds()
		fmt.Printf("  Average throughput:   %s/s\n", formatBytes(int64(throughput)))
	}

	fmt.Println(strings.Repeat("=", separatorLineLength))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (t *tester) run(ctx context.Context) error {
	steps := t.getTestSteps()
	stepsRun := 0

	for _, step := range steps {
		if step.shouldRun {
			if err := step.runFunc(ctx); err != nil {
				return err
			}
			stepsRun++
		}
	}

	if stepsRun == 0 {
		fmt.Println("No test steps were selected. Use -h for help.")
		return errors.New("no tests selected")
	}

	return nil
}

type testStep struct {
	shouldRun bool
	runFunc   func(context.Context) error
}

func (t *tester) getTestSteps() []testStep {
	return []testStep{
		{t.cfg.runSinglePass, t.runSinglePassStep},
		{t.cfg.runMultiPass, t.runMultiPassStep},
		{t.cfg.runParallelInfoSame, t.runParallelInfoSameStep},
		{t.cfg.runParallelInfoDiff, t.runParallelInfoDiffStep},
		{t.cfg.runFullParallel, t.runFullParallelStep},
	}
}

func (t *tester) runSinglePassStep(ctx context.Context) error {
	fmt.Println("Step 1: Running single pass")
	t.metrics.startStep("Step 1: Single pass")
	_, err := t.performPass(ctx, false)
	t.metrics.endStep(err)
	if err != nil {
		return fmt.Errorf("single pass failed: %w", err)
	}
	fmt.Println("✓ Step 1 completed successfully")
	return nil
}

func (t *tester) runMultiPassStep(ctx context.Context) error {
	fmt.Printf("\nStep 2: Running %d sequential passes\n", t.cfg.multiPassCount)
	t.metrics.startStep(fmt.Sprintf("Step 2: %d sequential passes", t.cfg.multiPassCount))

	for i := 1; i <= t.cfg.multiPassCount; i++ {
		fmt.Printf("  Pass %d/%d...\n", i, t.cfg.multiPassCount)
		if _, err := t.performPass(ctx, false); err != nil {
			t.metrics.endStep(err)
			return fmt.Errorf("sequential pass %d failed: %w", i, err)
		}
	}
	t.metrics.endStep(nil)
	fmt.Println("✓ Step 2 completed successfully")
	return nil
}

func (t *tester) runParallelInfoSameStep(ctx context.Context) error {
	fmt.Printf("\nStep 3: Running %d parallel /info requests for a single file\n", t.cfg.parallelInfoCount)
	t.metrics.startStep(fmt.Sprintf("Step 3: %d parallel /info for same file", t.cfg.parallelInfoCount))

	result, err := t.performPass(ctx, true)
	if err != nil {
		t.metrics.endStep(err)
		return fmt.Errorf("preparing file for parallel info failed: %w", err)
	}

	if err := t.parallelInfoRequests(ctx, result.Hash, t.cfg.parallelInfoCount); err != nil {
		_ = t.deleteRemoteFile(ctx, result.Hash)
		t.metrics.endStep(err)
		return fmt.Errorf("parallel /info (same file) failed: %w", err)
	}

	if err := t.deleteRemoteFile(ctx, result.Hash); err != nil {
		t.metrics.endStep(err)
		return fmt.Errorf("cleanup after parallel info failed: %w", err)
	}

	t.metrics.endStep(nil)
	fmt.Println("✓ Step 3 completed successfully")
	return nil
}

func (t *tester) runParallelInfoDiffStep(ctx context.Context) error {
	fmt.Printf("\nStep 4: Running %d parallel /info requests for different files\n", t.cfg.parallelInfoCount)
	t.metrics.startStep(fmt.Sprintf("Step 4: %d parallel /info for different files", t.cfg.parallelInfoCount))

	hashes := make([]string, 0, t.cfg.parallelInfoCount)
	for i := range t.cfg.parallelInfoCount {
		result, err := t.performPass(ctx, true)
		if err != nil {
			t.cleanupHashes(ctx, hashes)
			t.metrics.endStep(err)
			return fmt.Errorf("preparing file %d for parallel info failed: %w", i+1, err)
		}
		hashes = append(hashes, result.Hash)
	}

	if err := t.parallelInfoDifferent(ctx, hashes); err != nil {
		t.cleanupHashes(ctx, hashes)
		t.metrics.endStep(err)
		return fmt.Errorf("parallel /info (different files) failed: %w", err)
	}

	t.cleanupHashes(ctx, hashes)
	t.metrics.endStep(nil)
	fmt.Println("✓ Step 4 completed successfully")
	return nil
}

func (t *tester) runFullParallelStep(ctx context.Context) error {
	fmt.Printf("\nStep 5: Running %d full passes in parallel\n", t.cfg.fullPassConcurrent)
	t.metrics.startStep(fmt.Sprintf("Step 5: %d full passes in parallel", t.cfg.fullPassConcurrent))

	err := runParallel(t.cfg.fullPassConcurrent, func(int) error {
		_, testErr := t.performPass(ctx, false)
		return testErr
	})

	t.metrics.endStep(err)
	if err != nil {
		return fmt.Errorf("parallel passes failed: %w", err)
	}

	fmt.Println("✓ Step 5 completed successfully")
	return nil
}

func (t *tester) performPass(ctx context.Context, keepRemote bool) (*passResult, error) {
	data, expectedHash, err := t.prepareTestData()
	if err != nil {
		return nil, err
	}

	hash, err := t.uploadFile(ctx, data)
	if err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup && hash != "" {
			if deleteErr := t.deleteRemoteFile(ctx, hash); deleteErr != nil {
				fmt.Fprintf(os.Stderr, "failed to cleanup remote hash %s: %v\n", hash, deleteErr)
			}
		}
	}()

	if err := t.verifyUploadedFile(ctx, hash, expectedHash, data); err != nil {
		return nil, err
	}

	return t.finishPass(ctx, hash, keepRemote, &cleanup)
}

func (t *tester) prepareTestData() ([]byte, string, error) {
	data := make([]byte, t.cfg.fileSize)
	if _, err := rand.Read(data); err != nil {
		return nil, "", fmt.Errorf("generate random data: %w", err)
	}

	sum := sha256.Sum256(data)
	expectedHash := hex.EncodeToString(sum[:])
	return data, expectedHash, nil
}

func (t *tester) verifyUploadedFile(ctx context.Context, hash, expectedHash string, data []byte) error {
	if hash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, hash)
	}

	if err := t.fetchInfo(ctx, hash); err != nil {
		return err
	}

	return t.verifyDownload(ctx, hash, data)
}

func (t *tester) finishPass(ctx context.Context, hash string, keepRemote bool, cleanup *bool) (*passResult, error) {
	if keepRemote {
		*cleanup = false
		return &passResult{Hash: hash}, nil
	}

	if err := t.deleteRemoteFile(ctx, hash); err != nil {
		return nil, err
	}
	*cleanup = false
	return &passResult{Hash: hash}, nil
}

func (t *tester) uploadFile(ctx context.Context, data []byte) (string, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		t.metrics.recordOperation("upload", duration, int64(len(data)), nil)
	}()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", fmt.Sprintf("cas-test-%d.bin", time.Now().UnixNano()))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write multipart data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	var uploadResp uploadResponse
	if err := t.client.doJSON(ctx, http.MethodPost, "/file/upload", &buf, writer.FormDataContentType(), &uploadResp); err != nil {
		t.metrics.recordOperation("upload", time.Since(start), int64(len(data)), err)
		return "", fmt.Errorf("upload failed: %w", err)
	}

	if uploadResp.Hash == "" {
		err := errors.New("upload response missing hash")
		t.metrics.recordOperation("upload", time.Since(start), int64(len(data)), err)
		return "", err
	}

	return uploadResp.Hash, nil
}

func (t *tester) fetchInfo(ctx context.Context, hash string) error {
	start := time.Now()
	path := fmt.Sprintf("/file/%s/info", hash)
	var info fileInfoResponse
	if err := t.client.doJSON(ctx, http.MethodGet, path, nil, "", &info); err != nil {
		t.metrics.recordOperation("info", time.Since(start), 0, err)
		return fmt.Errorf("fetch info failed: %w", err)
	}

	if !strings.EqualFold(info.Hash, hash) {
		err := fmt.Errorf("info hash mismatch: expected %s, got %s", hash, info.Hash)
		t.metrics.recordOperation("info", time.Since(start), 0, err)
		return err
	}

	t.metrics.recordOperation("info", time.Since(start), 0, nil)
	return nil
}

func (t *tester) verifyDownload(ctx context.Context, hash string, expected []byte) error {
	start := time.Now()
	path := fmt.Sprintf("/file/%s/download", hash)
	body, err := t.client.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		t.metrics.recordOperation("download", time.Since(start), 0, err)
		return fmt.Errorf("download failed: %w", err)
	}

	if !bytes.Equal(body, expected) {
		err := errors.New("downloaded data mismatch")
		t.metrics.recordOperation("download", time.Since(start), int64(len(body)), err)
		return err
	}

	t.metrics.recordOperation("download", time.Since(start), int64(len(body)), nil)
	return nil
}

func (t *tester) deleteRemoteFile(ctx context.Context, hash string) error {
	start := time.Now()
	path := fmt.Sprintf("/file/%s/delete", hash)
	_, err := t.client.doRequest(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		// Check if it's a 204 No Content response which is also valid
		if strings.Contains(err.Error(), "204") {
			t.metrics.recordOperation("delete", time.Since(start), 0, nil)
			return nil
		}
		t.metrics.recordOperation("delete", time.Since(start), 0, err)
		return fmt.Errorf("delete failed: %w", err)
	}
	t.metrics.recordOperation("delete", time.Since(start), 0, nil)
	return nil
}

func (t *tester) parallelInfoRequests(ctx context.Context, hash string, count int) error {
	return runParallel(count, func(int) error {
		return t.fetchInfo(ctx, hash)
	})
}

func (t *tester) parallelInfoDifferent(ctx context.Context, hashes []string) error {
	return runParallel(len(hashes), func(i int) error {
		return t.fetchInfo(ctx, hashes[i])
	})
}

func (t *tester) cleanupHashes(ctx context.Context, hashes []string) {
	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		if err := t.deleteRemoteFile(ctx, hash); err != nil {
			fmt.Fprintf(os.Stderr, "failed to cleanup hash %s: %v\n", hash, err)
		}
	}
}

func runParallel(count int, function func(int) error) error {
	var waitGroup sync.WaitGroup
	errCh := make(chan error, count)

	for index := range count {
		waitGroup.Add(1)
		go func(idx int) {
			defer waitGroup.Done()
			if err := function(idx); err != nil {
				errCh <- err
			}
		}(index)
	}

	waitGroup.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}
