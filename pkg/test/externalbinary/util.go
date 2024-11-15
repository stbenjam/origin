package externalbinary

import (
	"compress/gzip"
	"crypto/sha1"
	"debug/elf"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/openshift/origin/test/extended/util"
)

// ungzipFile checks if a binary is gzipped (ends with .gz) and decompresses it.
// Returns the new filename of the decompressed file (original is deleted), or original filename if it was not gzipped.
func ungzipFile(extractedBinary string) (string, error) {

	if strings.HasSuffix(extractedBinary, ".gz") {

		gzFile, err := os.Open(extractedBinary)
		if err != nil {
			return "", fmt.Errorf("failed to open gzip file: %w", err)
		}
		defer gzFile.Close()

		gzipReader, err := gzip.NewReader(gzFile)
		if err != nil {
			return "", fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzipReader.Close()

		newFilePath := strings.TrimSuffix(extractedBinary, ".gz")
		outFile, err := os.Create(newFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to create output file: %w", err)
		}
		defer outFile.Close()

		if _, err := io.Copy(outFile, gzipReader); err != nil {
			return "", fmt.Errorf("failed to write to output file: %w", err)
		}

		// Attempt to delete the original .gz file
		if err := os.Remove(extractedBinary); err != nil {
			return "", fmt.Errorf("failed to delete original .gz file: %w", err)
		}

		return newFilePath, nil
	}

	// Return the original path if the file was not decompressed
	return extractedBinary, nil
}

// Checks whether the binary has a compatible CPU architecture  to the
// host.
func checkCompatibleArchitecture(executablePath string) error {
	file, err := os.Open(executablePath)
	if err != nil {
		return fmt.Errorf("failed to open ELF file: %w", err)
	}
	defer file.Close()

	elfFile, err := elf.NewFile(file)
	if err != nil {
		return fmt.Errorf("failed to parse ELF file: %w", err)
	}

	// Determine the architecture of the ELF file
	elfArch := elfFile.Machine
	var expectedArch elf.Machine

	// Determine the host architecture
	switch runtime.GOARCH {
	case "amd64":
		expectedArch = elf.EM_X86_64
	case "arm64":
		expectedArch = elf.EM_AARCH64
	case "s390x":
		expectedArch = elf.EM_S390
	case "ppc64le":
		expectedArch = elf.EM_PPC64
	default:
		return fmt.Errorf("unsupported host architecture: %s", runtime.GOARCH)
	}

	if elfArch != expectedArch {
		return fmt.Errorf("binary architecture %q doesn't matched expected architecture %q", elfArch, expectedArch)
	}

	return nil
}

// runImageExtract extracts src from specified image to dst
func runImageExtract(image, src, dst string, dockerConfigJsonPath string, logger *log.Logger) error {
	var err error
	var out []byte
	maxRetries := 6
	startTime := time.Now()
	logger.Printf("Run image extract for release image %q and src %q at %v", image, src, startTime)
	for i := 1; i <= maxRetries; i++ {
		args := []string{"--kubeconfig=" + util.KubeConfigPath(), "image", "extract", image, fmt.Sprintf("--path=%s:%s", src, dst), "--confirm"}
		if len(dockerConfigJsonPath) > 0 {
			args = append(args, fmt.Sprintf("--registry-config=%s", dockerConfigJsonPath))
		}
		cmd := exec.Command("oc", args...)
		out, err = cmd.CombinedOutput()
		if err != nil {
			// Allow retries for up to one minute. The openshift internal registry
			// occasionally reports "manifest unknown" when a new image has just
			// been exposed through an imagestream.
			time.Sleep(10 * time.Second)
			continue
		}
		extractionTime := time.Since(startTime)
		logger.Printf("Run image extract for release image %q at %v", image, extractionTime)
		return nil
	}
	return fmt.Errorf("error during image extract: %w (%v)", err, string(out))
}

// pullSpecToDirName converts a release pullspec to a directory, for use with caching.
func pullSpecToDirName(input string) string {
	// Remove any non-alphanumeric characters (except '-') and replace them with '_'.
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	safeName := re.ReplaceAllString(input, "_")

	// Truncate long names
	if len(safeName) > 249 {
		safeName = safeName[:249]
	}

	// Add suffix to avoid collision when truncating
	hash := sha1.Sum([]byte(input))
	safeName += fmt.Sprintf("_%x", hash[:6])

	// Return a clean, safe directory path.
	return filepath.Clean(safeName)
}
