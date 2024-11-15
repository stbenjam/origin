package externalbinary

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/pkg/errors"

	"github.com/openshift/origin/test/extended/util"
)

type CachedReleaseBinaryProvider struct {
	oc                   *util.CLI
	cachePath            string
	logger               *log.Logger
	registryAuthFilePath string
	imageStream          *imagev1.ImageStream
}

func NewCachedExternalBinaryProvider(logger *log.Logger, releaseImage,
	registryAuthfilePath string) (*CachedReleaseBinaryProvider,
	error) {
	oc := util.NewCLIWithoutNamespace("default")

	// Determine cache path
	cacheBase := os.Getenv("XDG_CACHE_HOME")
	if cacheBase == "" {
		cacheBase = path.Join(os.Getenv("HOME"), ".cache", "openshift-tests")
	}
	cachePath := path.Join(cacheBase, pullSpecToDirName(releaseImage))
	// Ensure the cache directory exists, is writable, and executable
	if err := createCachePath(cachePath); err != nil {
		return nil, errors.WithMessagef(err, "error creating cache path %q", cachePath)
	}
	logger.Printf("Using cache path %q", cachePath)

	// Get releasePayloadImageStream
	releasePayloadImageStream, releaseImage, err := extractReleaseImageStream(logger, cachePath,
		releaseImage, registryAuthfilePath)
	if err != nil {
		return nil, errors.WithMessage(err, "couldn't extract release payload image stream")
	}

	return &CachedReleaseBinaryProvider{
		registryAuthFilePath: registryAuthfilePath,
		logger:               logger,
		oc:                   oc,
		imageStream:          releasePayloadImageStream,
		cachePath:            cachePath,
	}, nil
}

// ExtractBinaryFromReleaseImage resolves the tag from the release image and extracts the binary,
// returning the path to the binary or an error if extraction fails.
func (ebp *CachedReleaseBinaryProvider) ExtractBinaryFromReleaseImage(tag, binary string) (*TestBinary, error) {
	// Resolve the image tag from the image stream.
	image := ""
	for _, t := range ebp.imageStream.Spec.Tags {
		if t.Name == tag {
			image = t.From.Name
			break
		}
	}

	if len(image) == 0 {
		return nil, fmt.Errorf("%s not found", tag)
	}

	// Define the path for the cached binary.
	cachedBinary := filepath.Join(ebp.cachePath, strings.TrimSuffix(filepath.Base(binary), ".gz"))

	// Check if the binary already exists in the cache.
	if _, err := os.Stat(cachedBinary); err == nil {
		ebp.logger.Printf("Using cached binary %q for tag %q", cachedBinary, tag)
		return &TestBinary{
			logger: ebp.logger,
			path:   cachedBinary,
		}, nil
	}

	// Start the extraction process.
	startTime := time.Now()
	if err := runImageExtract(image, binary, ebp.cachePath, ebp.registryAuthFilePath, ebp.logger); err != nil {
		return nil, fmt.Errorf("failed extracting %q from %q: %w", binary, image, err)
	}
	extractDuration := time.Since(startTime)

	// Construct the path to the extracted binary.
	extractedBinary := filepath.Join(ebp.cachePath, filepath.Base(binary))

	// Support gzipped external binaries (handle decompression).
	extractedBinary, err := ungzipFile(extractedBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress external binary %q: %w", binary, err)
	}

	// Make the extracted binary executable.
	if err := os.Chmod(extractedBinary, 0755); err != nil {
		return nil, fmt.Errorf("failed making the extracted binary %q executable: %w", extractedBinary, err)
	}

	// Verify the binary file exists and get its stats.
	fileInfo, err := os.Stat(extractedBinary)
	if err != nil {
		return nil, fmt.Errorf("failed stat on extracted binary %q: %w", extractedBinary, err)
	}

	// Verify the binary is compatible with our architecture
	if err := checkCompatibleArchitecture(extractedBinary); err != nil {
		return nil, errors.WithMessage(err, "error checking binary architecture compatability")
	}

	ebp.logger.Printf("Extracted %q for tag %q from %q (disk size %v, extraction duration %v)",
		binary, tag, image, fileInfo.Size(), extractDuration)

	return &TestBinary{
		logger: ebp.logger,
		path:   extractedBinary,
	}, nil
}
