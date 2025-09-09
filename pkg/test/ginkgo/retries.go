package ginkgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Retry constants
const (
	maxIntraRunRetryDuration = 2 * time.Minute
	maxTotalTestFailures     = 5
	maxIntraRunRetryAttempts = 10
	intraRunFlakeThreshold   = 4
	defaultRetryStrategy     = "aggressive"
)

// RetryOutcome represents the decision for a multi-retry test
type RetryOutcome int

const (
	RetryOutcomeFail RetryOutcome = iota
	RetryOutcomeFlaky
	RetryOutcomeSkipped
)

// RetryStrategy controls both retry behavior and final outcome decisions
// Example usage:
//
//	options.RetryStrategy = NewRetryOnceStrategy()                          // Restrictive retry with rules
//	options.RetryStrategy = NewAggressiveRetryStrategy(10, 4)               // Aggressive multiple retries
type RetryStrategy interface {
	// Name returns the strategy name for CLI and logging
	Name() string

	// Should we attempt any retries given the list of failing tests?
	ShouldAttemptRetries(failing []*testCase, suite *TestSuite) bool

	// How many retries are planned? (for reporting/planning)
	GetMaxRetries(testCase *testCase) int

	// Should we continue retrying? (for actual control)
	ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool

	// What's the final outcome after all attempts?
	DecideOutcome(testName string, attempts []*testCase) RetryOutcome
}

// RetryOnceStrategy implements the restrictive "once" retry behavior
type RetryOnceStrategy struct {
	PermittedRetryImageTags []string
}

// NewRetryOnceStrategy creates a strategy that retries failed tests once with restrictions
func NewRetryOnceStrategy() *RetryOnceStrategy {
	return &RetryOnceStrategy{
		PermittedRetryImageTags: []string{"tests"}, // tests = openshift-tests image
	}
}

// Name implements RetryStrategy
func (s *RetryOnceStrategy) Name() string {
	return "once"
}

// ShouldAttemptRetries implements RetryStrategy
func (s *RetryOnceStrategy) ShouldAttemptRetries(failing []*testCase, suite *TestSuite) bool {
	return len(failing) > 0 && len(failing) <= suite.MaximumAllowedFlakes
}

// GetMaxRetries implements RetryStrategy
func (s *RetryOnceStrategy) GetMaxRetries(testCase *testCase) int {
	if s.shouldRetryTest(testCase) {
		return 1
	}
	return 0
}

// ShouldContinue implements RetryStrategy
func (s *RetryOnceStrategy) ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool {
	// Stop after first retry
	if attemptNumber >= 2 {
		return false
	}

	// Check if test is eligible for retry based on image restrictions
	if !s.shouldRetryTest(testCase) {
		return false
	}

	// Allow one retry for failed tests
	lastAttempt := allAttempts[len(allAttempts)-1]
	return lastAttempt.failed
}

// DecideOutcome implements RetryStrategy
func (s *RetryOnceStrategy) DecideOutcome(testName string, attempts []*testCase) RetryOutcome {
	for _, attempt := range attempts {
		if attempt.skipped {
			return RetryOutcomeSkipped
		}
		if attempt.success {
			return RetryOutcomeFlaky
		}
	}
	return RetryOutcomeFail
}

// shouldRetryTest determines if a failed test should be retried based on retry policies.
// It returns true if the test is eligible for retry, false otherwise.
func (s *RetryOnceStrategy) shouldRetryTest(test *testCase) bool {
	// Internal tests (no binary) are eligible for retry, we shouldn't really have any of these
	// now that origin is also an extension.
	if test.binary == nil {
		return true
	}

	tlog := logrus.WithField("test", test.name)

	// Test retries were disabled for some suites when they moved to OTE. This exposed small numbers of tests that
	// were actually flaky and nobody knew. We attempted to fix these, a few did not make it in time. Restore
	// retries for specific test names so the overall suite can continue to not retry.
	retryTestNames := []string{
		"[sig-instrumentation] Metrics should grab all metrics from kubelet /metrics/resource endpoint [Suite:openshift/conformance/parallel] [Suite:k8s]", // https://issues.redhat.com/browse/OCPBUGS-57477
		"[sig-network] Services should be rejected for evicted pods (no endpoints exist) [Suite:openshift/conformance/parallel] [Suite:k8s]",               // https://issues.redhat.com/browse/OCPBUGS-57665
		"[sig-node] Pods Extended Pod Container lifecycle evicted pods should be terminal [Suite:openshift/conformance/parallel] [Suite:k8s]",              // https://issues.redhat.com/browse/OCPBUGS-57658
	}
	for _, rtn := range retryTestNames {
		if test.name == rtn {
			tlog.Debug("test has an exception allowing retry")
			return true
		}
	}

	// Get extension info to check if it's from a permitted image
	info, err := test.binary.Info(context.Background())
	if err != nil {
		tlog.WithError(err).
			Debug("Failed to get binary info, skipping retry")
		return false
	}

	// Check if the test's source image is in the permitted retry list
	for _, permittedTag := range s.PermittedRetryImageTags {
		if strings.Contains(info.Source.SourceImage, permittedTag) {
			tlog.WithField("image", info.Source.SourceImage).
				Debug("Permitting retry")
			return true
		}
	}

	tlog.WithField("image", info.Source.SourceImage).
		Debug("Test not eligible for retry based on image tag")
	return false
}

// AggressiveRetryStrategy implements the multiple retry behavior with fixed failure threshold
type AggressiveRetryStrategy struct {
	maxRetries       int
	failureThreshold int
}

// NewAggressiveRetryStrategy creates a strategy that retries tests multiple times
func NewAggressiveRetryStrategy(maxRetries, failureThreshold int) *AggressiveRetryStrategy {
	return &AggressiveRetryStrategy{
		maxRetries:       maxRetries,
		failureThreshold: failureThreshold,
	}
}

// Name implements RetryStrategy
func (s *AggressiveRetryStrategy) Name() string {
	return "aggressive"
}

// ShouldAttemptRetries implements RetryStrategy
func (s *AggressiveRetryStrategy) ShouldAttemptRetries(failing []*testCase, suite *TestSuite) bool {
	return len(failing) > 0 && len(failing) <= maxTotalTestFailures
}

// GetMaxRetries implements RetryStrategy
func (s *AggressiveRetryStrategy) GetMaxRetries(testCase *testCase) int {
	// Skip retries for tests that exceed duration limit
	if testCase.duration >= maxIntraRunRetryDuration {
		return 0
	}
	return s.maxRetries
}

// ShouldContinue implements RetryStrategy
func (s *AggressiveRetryStrategy) ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool {
	// Stop if we've hit max attempts
	if attemptNumber > s.maxRetries {
		return false
	}

	// Skip retries for tests that exceed duration limit
	if testCase.duration >= maxIntraRunRetryDuration {
		return false
	}

	// In multi-retry mode, continue until we reach max attempts regardless of results
	return true
}

// DecideOutcome implements RetryStrategy
func (s *AggressiveRetryStrategy) DecideOutcome(testName string, attempts []*testCase) RetryOutcome {
	failureCount := 0
	skippedCount := 0

	for _, attempt := range attempts {
		if attempt.failed {
			failureCount++
		} else if attempt.skipped {
			skippedCount++
		}
	}

	// Only consider skipped if majority of attempts were skipped
	if skippedCount > len(attempts)/2 {
		return RetryOutcomeSkipped
	}

	if failureCount < s.failureThreshold {
		return RetryOutcomeFlaky
	}

	return RetryOutcomeFail
}

// NoRetryStrategy implements a no-retry policy
type NoRetryStrategy struct{}

func (s *NoRetryStrategy) Name() string { return "none" }
func (s *NoRetryStrategy) ShouldAttemptRetries(failing []*testCase, suite *TestSuite) bool {
	return false
}
func (s *NoRetryStrategy) GetMaxRetries(testCase *testCase) int { return 0 }
func (s *NoRetryStrategy) ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool {
	return false
}
func (s *NoRetryStrategy) DecideOutcome(testName string, attempts []*testCase) RetryOutcome {
	for _, attempt := range attempts {
		if attempt.skipped {
			return RetryOutcomeSkipped
		}
		if attempt.success {
			return RetryOutcomeFlaky
		}
	}
	return RetryOutcomeFail
}

// GetAvailableRetryStrategies returns a list of available strategy names
func GetAvailableRetryStrategies() []string {
	return []string{"once", "aggressive", "none"}
}

// CreateRetryStrategy creates a strategy by name
func CreateRetryStrategy(name string) (RetryStrategy, error) {
	switch name {
	case "once":
		return NewRetryOnceStrategy(), nil
	case "aggressive":
		return NewAggressiveRetryStrategy(maxIntraRunRetryAttempts, intraRunFlakeThreshold), nil
	case "none":
		return &NoRetryStrategy{}, nil
	default:
		return nil, fmt.Errorf("unknown retry strategy: %s (available: %v)", name, GetAvailableRetryStrategies())
	}
}
