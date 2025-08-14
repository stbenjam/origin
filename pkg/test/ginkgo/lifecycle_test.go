package ginkgo

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"

	"github.com/openshift/origin/pkg/test/extensions"
)

func TestProcessTestResults(t *testing.T) {
	testCases := []struct {
		name                   string
		tests                  []*testCase
		expectedPass           int
		expectedFail           int
		expectedSkip           int
		expectedFlakes         int
		expectedInformingCount int
		expectedBlockingCount  int
		expectedOutputContains []string
	}{
		{
			name:                   "no failures",
			tests:                  []*testCase{},
			expectedPass:           0,
			expectedFail:           0,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 0,
			expectedBlockingCount:  0,
		},
		{
			name: "only blocking failures",
			tests: []*testCase{
				{
					name:   "blocking-test-1",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleBlocking,
						},
					},
					testOutputBytes: []byte("original failure output"),
				},
				{
					name:   "blocking-test-2",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleBlocking,
						},
					},
				},
			},
			expectedPass:           0,
			expectedFail:           2,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 0,
			expectedBlockingCount:  2,
		},
		{
			name: "only informing failures",
			tests: []*testCase{
				{
					name:   "informing-test-1",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleInforming,
						},
					},
					testOutputBytes: []byte("original failure output"),
				},
				{
					name:   "informing-test-2",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleInforming,
						},
					},
				},
			},
			expectedPass:           0,
			expectedFail:           2,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 2,
			expectedBlockingCount:  0,
			expectedOutputContains: []string{
				"*** NOTE: This test's lifecycle is INFORMING",
				"doesn't contribute to the overall success or failure state",
			},
		},
		{
			name: "mixed failures",
			tests: []*testCase{
				{
					name:   "blocking-test",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleBlocking,
						},
					},
				},
				{
					name:   "informing-test",
					failed: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleInforming,
						},
					},
					testOutputBytes: []byte("original failure output"),
				},
			},
			expectedPass:           0,
			expectedFail:           2,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 1,
			expectedBlockingCount:  1,
			expectedOutputContains: []string{
				"*** NOTE: This test's lifecycle is INFORMING",
			},
		},
		{
			name: "test without extension spec (legacy test)",
			tests: []*testCase{
				{
					name:              "legacy-test",
					failed:            true,
					extensionTestSpec: nil, // No extension spec
				},
			},
			expectedPass:           0,
			expectedFail:           1,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 0,
			expectedBlockingCount:  1,
		},
		{
			name: "successful tests should not be counted",
			tests: []*testCase{
				{
					name:    "successful-test",
					failed:  false,
					success: true,
					extensionTestSpec: &extensions.ExtensionTestSpec{
						ExtensionTestSpec: &extensiontests.ExtensionTestSpec{
							Lifecycle: extensiontests.LifecycleBlocking,
						},
					},
				},
			},
			expectedPass:           1,
			expectedFail:           0,
			expectedSkip:           0,
			expectedFlakes:         0,
			expectedInformingCount: 0,
			expectedBlockingCount:  0,
		},
		{
			name: "flaky test detection",
			tests: []*testCase{
				{
					name:    "flaky-test",
					failed:  true,
					success: false,
				},
				{
					name:    "flaky-test", // Same test name, different result
					failed:  false,
					success: true,
				},
			},
			expectedPass:           1,
			expectedFail:           1,
			expectedSkip:           0,
			expectedFlakes:         1,
			expectedInformingCount: 0,
			expectedBlockingCount:  1, // Failed test without extension spec is blocking
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			testDuration := 5 * time.Second
			summary := processTestResults(tc.tests, testDuration, &buf)

			if summary.Pass != tc.expectedPass {
				t.Errorf("Expected Pass=%d, got %d", tc.expectedPass, summary.Pass)
			}

			if summary.Fail != tc.expectedFail {
				t.Errorf("Expected Fail=%d, got %d", tc.expectedFail, summary.Fail)
			}

			if summary.Skip != tc.expectedSkip {
				t.Errorf("Expected Skip=%d, got %d", tc.expectedSkip, summary.Skip)
			}

			if len(summary.Flakes) != tc.expectedFlakes {
				t.Errorf("Expected Flakes=%d, got %d", tc.expectedFlakes, len(summary.Flakes))
			}

			if summary.InformingFailures != tc.expectedInformingCount {
				t.Errorf("Expected InformingFailures=%d, got %d", tc.expectedInformingCount, summary.InformingFailures)
			}

			if summary.BlockingFailures != tc.expectedBlockingCount {
				t.Errorf("Expected BlockingFailures=%d, got %d", tc.expectedBlockingCount, summary.BlockingFailures)
			}

			if len(summary.FailingTests) != tc.expectedFail {
				t.Errorf("Expected %d failing tests, got %d", tc.expectedFail, len(summary.FailingTests))
			}

			// Check that informing message was injected into test output
			for _, expectedContent := range tc.expectedOutputContains {
				found := false
				for _, test := range tc.tests {
					if test.failed && strings.Contains(string(test.testOutputBytes), expectedContent) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find '%s' in test output, but didn't", expectedContent)
				}
			}

			// Verify that informing tests have the message prepended
			for _, test := range tc.tests {
				if test.failed && test.extensionTestSpec != nil &&
					test.extensionTestSpec.ExtensionTestSpec != nil &&
					test.extensionTestSpec.ExtensionTestSpec.Lifecycle == extensiontests.LifecycleInforming {

					output := string(test.testOutputBytes)
					if !strings.HasPrefix(output, "*** NOTE: This test's lifecycle is INFORMING") {
						t.Errorf("Informing test '%s' should have informing message prepended to output", test.name)
					}
				}
			}
		})
	}
}

func TestTestResultSummaryString(t *testing.T) {
	testCases := []struct {
		name     string
		summary  testResultSummary
		expected string
	}{
		{
			name: "no failures",
			summary: testResultSummary{
				Pass:     10,
				Skip:     2,
				Duration: 5 * time.Second,
			},
			expected: "10 pass, 2 skip (5s)",
		},
		{
			name: "blocking failures",
			summary: testResultSummary{
				Pass:              8,
				Fail:              3,
				Skip:              1,
				BlockingFailures:  2,
				InformingFailures: 1,
				Duration:          10 * time.Second,
			},
			expected: "3 fail (2 blocking, 1 informing), 8 pass, 1 skip (10s)",
		},
		{
			name: "only informing failures",
			summary: testResultSummary{
				Pass:              8,
				Fail:              2,
				Skip:              1,
				BlockingFailures:  0,
				InformingFailures: 2,
				Duration:          7 * time.Second,
			},
			expected: "2 informing failures detected, but no blocking failures - 8 pass, 1 skip (7s)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.summary.String()
			if result != tc.expected {
				t.Errorf("Expected: %q, got: %q", tc.expected, result)
			}
		})
	}
}
