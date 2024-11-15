package externalbinary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

type externalBinaryStruct struct {
	// The payload image tag in which an external binary path can be found
	imageTag string
	// The binary path to extract from the image
	binaryPath string
}

var externalBinaries = []externalBinaryStruct{
	{
		imageTag:   "hyperkube",
		binaryPath: "/usr/bin/k8s-tests-ext.gz",
	},
}

func ExtractAllTestBinaries(logger *log.Logger, parallelism int) (TestBinaries, error) {
	if parallelism < 1 {
		return nil, errors.New("parallelism must be greater than zero")
	}

	externalBinaryProvider, err := NewCachedExternalBinaryProvider(logger)
	if err != nil {
		return nil, errors.WithMessage(err, "could not create external binary provider")
	}

	var (
		binaries []*TestBinary
		mu       sync.Mutex
		wg       sync.WaitGroup
		errCh    = make(chan error, 1)
		jobCh    = make(chan externalBinaryStruct)
	)

	// Producer: sends jobs to the jobCh channel
	go func() {
		defer close(jobCh)
		for _, b := range externalBinaries {
			jobCh <- b
		}
	}()

	// Consumer workers: extract test binaries concurrently
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range jobCh {
				testBinary, err := externalBinaryProvider.ExtractBinaryFromReleaseImage(b.imageTag, b.binaryPath)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				mu.Lock()
				binaries = append(binaries, testBinary)
				mu.Unlock()
			}
		}()
	}

	// Wait for all workers to finish
	wg.Wait()
	close(errCh)

	// Check if any errors were reported
	if err := <-errCh; err != nil {
		return nil, err
	}

	return binaries, nil
}

type TestBinaries []*TestBinary

// TestsForSuite extracts the tests from all TestBinaries using the specified parallelism.
func (binaries TestBinaries) TestsForSuite(ctx context.Context, parallelism int) (ExtensionTestSpecs, error) {
	var (
		allTests ExtensionTestSpecs
		mu       sync.Mutex
		wg       sync.WaitGroup
		errCh    = make(chan error, 1)
		jobCh    = make(chan *TestBinary)
	)

	// Producer: sends jobs to the jobCh channel
	go func() {
		defer close(jobCh)
		for _, binary := range binaries {
			jobCh <- binary
		}
	}()

	// Consumer workers: extract tests concurrently
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for binary := range jobCh {
				tests, err := binary.TestsForSuite(ctx)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				mu.Lock()
				allTests = append(allTests, tests...)
				mu.Unlock()
			}
		}()
	}

	// Wait for all workers to finish
	wg.Wait()
	close(errCh)

	// Check if any errors were reported
	if err := <-errCh; err != nil {
		return nil, err
	}

	return allTests, nil
}

type TestBinary struct {
	path   string
	logger *log.Logger
}

// TestsForSuite reads JSONL output from the binary and returns the parsed tests.
func (b *TestBinary) TestsForSuite(ctx context.Context) (ExtensionTestSpecs, error) {
	var tests ExtensionTestSpecs

	command := exec.Command(b.path, "list", "--output", "jsonl")
	testList, err := runWithTimeout(ctx, command, 1*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed running '%s list': %w", b.path, err)
	}

	buf := bytes.NewBuffer(testList)
	for {
		line, err := buf.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading output: %w", err)
		}

		// Trim whitespace and check if the line starts with '{'
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}

		// Parse JSONL line into a new ExtensionTestSpec instance
		testSpec := new(ExtensionTestSpec)
		err = json.Unmarshal([]byte(line), testSpec)
		if err != nil {
			return nil, fmt.Errorf("binary returned JSON-looking liket that didn't unmarshal: %q", line)
		}

		// Annotate the test spec with the binary path
		testSpec.Binary = b.path
		tests = append(tests, testSpec)
	}

	return tests, nil
}

func runWithTimeout(ctx context.Context, c *exec.Cmd, timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		go func() {
			select {
			// interrupt tests after timeout, and abort if they don't complete quick enough
			case <-time.After(timeout):
				if c.Process != nil {
					c.Process.Signal(syscall.SIGINT)
				}
				// if the process appears to be hung a significant amount of time after the timeout
				// send an ABRT so we get a stack dump
				select {
				case <-time.After(time.Minute):
					if c.Process != nil {
						c.Process.Signal(syscall.SIGABRT)
					}
				}
			case <-ctx.Done():
				if c.Process != nil {
					c.Process.Signal(syscall.SIGINT)
				}
			}

		}()
	}
	return c.CombinedOutput()
}
