package ginkgo

import "fmt"

// RetryOutcome represents the decision for a multi-retry test
type RetryOutcome int

const (
	RetryOutcomeFail RetryOutcome = iota
	RetryOutcomePass
	RetryOutcomeSkipped
)

// RetryStrategy controls both retry behavior and final outcome decisions
// Example usage:
//
//	options.RetryStrategy = NewRetryOnceStrategy()                          // Restrictive retry with rules
//	options.RetryStrategy = NewThresholdRetryStrategy(4)                    // Multiple retries with threshold
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
}

// NewRetryOnceStrategy creates a strategy that retries failed tests once with restrictions
func NewRetryOnceStrategy() *RetryOnceStrategy {
	return &RetryOnceStrategy{}
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
	// TODO: Add logic for test image restrictions and exceptions
	return 1
}

// ShouldContinue implements RetryStrategy
func (s *RetryOnceStrategy) ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool {
	// Stop after first retry
	if attemptNumber >= 2 {
		return false
	}

	// TODO: Add logic for test image restrictions and exceptions

	// For now, allow one retry for failed tests
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
			return RetryOutcomePass
		}
	}
	return RetryOutcomeFail
}

// ThresholdRetryStrategy implements the multiple retry behavior with fixed failure threshold
type ThresholdRetryStrategy struct {
	maxRetries       int
	failureThreshold int
}

// NewThresholdRetryStrategy creates a strategy that retries tests multiple times
func NewThresholdRetryStrategy(maxRetries, failureThreshold int) *ThresholdRetryStrategy {
	return &ThresholdRetryStrategy{
		maxRetries:       maxRetries,
		failureThreshold: failureThreshold,
	}
}

// Name implements RetryStrategy
func (s *ThresholdRetryStrategy) Name() string {
	return "threshold"
}

// ShouldAttemptRetries implements RetryStrategy
func (s *ThresholdRetryStrategy) ShouldAttemptRetries(failing []*testCase, suite *TestSuite) bool {
	return len(failing) > 0 && len(failing) <= maxTotalTestFailures
}

// GetMaxRetries implements RetryStrategy
func (s *ThresholdRetryStrategy) GetMaxRetries(testCase *testCase) int {
	// Skip retries for tests that exceed duration limit
	if testCase.duration >= maxIntraRunRetryDuration {
		return 0
	}
	return s.maxRetries
}

// ShouldContinue implements RetryStrategy
func (s *ThresholdRetryStrategy) ShouldContinue(testCase *testCase, allAttempts []*testCase, attemptNumber int) bool {
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
func (s *ThresholdRetryStrategy) DecideOutcome(testName string, attempts []*testCase) RetryOutcome {
	failureCount := 0
	skippedCount := 0

	for _, attempt := range attempts {
		if attempt.failed {
			failureCount++
		} else if attempt.skipped {
			skippedCount++
		}
	}

	if skippedCount > 0 {
		return RetryOutcomeSkipped
	}

	if failureCount < s.failureThreshold {
		return RetryOutcomePass
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
			return RetryOutcomePass
		}
	}
	return RetryOutcomeFail
}

// RetryStrategy registry for dynamic strategy selection
var retryStrategyRegistry = map[string]func() RetryStrategy{
	"once": func() RetryStrategy { return NewRetryOnceStrategy() },
	"threshold": func() RetryStrategy {
		return NewThresholdRetryStrategy(maxIntraRunRetryAttempts, intraRunFlakeThreshold)
	},
	"none": func() RetryStrategy { return &NoRetryStrategy{} },
}

// GetAvailableRetryStrategies returns a list of available strategy names
func GetAvailableRetryStrategies() []string {
	strategies := make([]string, 0, len(retryStrategyRegistry))
	for name := range retryStrategyRegistry {
		strategies = append(strategies, name)
	}
	return strategies
}

// CreateRetryStrategy creates a strategy by name
func CreateRetryStrategy(name string) (RetryStrategy, error) {
	factory, exists := retryStrategyRegistry[name]
	if !exists {
		return nil, fmt.Errorf("unknown retry strategy: %s (available: %v)", name, GetAvailableRetryStrategies())
	}
	return factory(), nil
}
