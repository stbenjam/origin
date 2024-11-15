package externalbinary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/pkg/errors"
	kapierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/origin/test/extended/util"
)

type ReleaseBinaryProvider interface {
	ExtractBinaryFromReleaseImage(string, string) (*TestBinary, error)
}

type CachedReleaseBinaryProvider struct {
	oc                   *util.CLI
	cachePath            string
	logger               *log.Logger
	registryAuthFilePath string
	imageStream          *imagev1.ImageStream
}

func NewCachedExternalBinaryProvider(logger *log.Logger) (*CachedReleaseBinaryProvider, error) {
	oc := util.NewCLIWithoutNamespace("default")

	registryAuthfilePath, err := getRegistryAuthFilePath(logger, oc)
	if err != nil {
		return nil, errors.WithMessage(err, "couldn't get registry auth file path")
	}

	// Determine which image we'll use
	releaseImage, err := determineReleasePayloadImage(logger)
	if err != nil {
		return nil, errors.WithMessage(err, "couldn't determine release image")
	}

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
	cachedBinary := filepath.Join(ebp.cachePath, filepath.Base(binary))

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

func determineReleasePayloadImage(logger *log.Logger) (string, error) {
	var releaseImage string

	// Highest priority override is EXTENSIONS_PAYLOAD_OVERRIDE
	overrideReleaseImage := os.Getenv("EXTENSIONS_PAYLOAD_OVERRIDE")
	if len(overrideReleaseImage) != 0 {
		// if "cluster" is specified, prefer target cluster payload even if RELEASE_IMAGE_LATEST is set.
		if overrideReleaseImage != "cluster" {
			releaseImage = overrideReleaseImage
			logger.Printf("Using env EXTENSIONS_PAYLOAD_OVERRIDE for release image %q", releaseImage)
		}
	} else {
		// Allow testing using an overridden source for external tests.
		envReleaseImage := os.Getenv("RELEASE_IMAGE_LATEST")
		if len(envReleaseImage) != 0 {
			releaseImage = envReleaseImage
			logger.Printf("Using env RELEASE_IMAGE_LATEST for release image %q", releaseImage)
		}
	}

	if len(releaseImage) == 0 {
		// Note that MicroShift does not have this resource. The test driver must use ENV vars.
		oc := util.NewCLIWithoutNamespace("default")
		cv, err := oc.AdminConfigClient().ConfigV1().ClusterVersions().Get(context.Background(), "version", metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed reading ClusterVersion/version: %w", err)
		}

		releaseImage = cv.Status.Desired.Image
		if len(releaseImage) == 0 {
			return "", fmt.Errorf("cannot determine release image from ClusterVersion resource")
		}
		logger.Printf("Using target cluster release image %q", releaseImage)
	}

	return releaseImage, nil
}

// extractReleaseImageStream extracts image references from the current
// cluster's release payload (or image specified by EXTENSIONS_PAYLOAD_OVERRIDE
// or RELEASE_IMAGE_LATEST which is used in OpenShift Test Platform CI) and returns
// an ImageStream object with tags associated with image-references from that payload.
func extractReleaseImageStream(logger *log.Logger, cachePath, releaseImage string,
	registryAuthFilePath string) (*imagev1.
	ImageStream,
	string, error) {

	if _, err := os.Stat(path.Join(cachePath, "image-references")); err != nil {
		if err := runImageExtract(releaseImage, "/release-manifests/image-references", cachePath, registryAuthFilePath,
			logger); err != nil {
			return nil, "", fmt.Errorf("failed extracting image-references from %q: %w", releaseImage, err)
		}
	}
	jsonFile, err := os.Open(filepath.Join(cachePath, "image-references"))
	if err != nil {
		return nil, "", fmt.Errorf("failed reading image-references from %q: %w", releaseImage, err)
	}
	defer jsonFile.Close()
	data, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, "", fmt.Errorf("unable to load release image-references from %q: %w", releaseImage, err)
	}
	is := &imagev1.ImageStream{}
	if err := json.Unmarshal(data, &is); err != nil {
		return nil, "", fmt.Errorf("unable to load release image-references from %q: %w", releaseImage, err)
	}
	if is.Kind != "ImageStream" || is.APIVersion != "image.openshift.io/v1" {
		return nil, "", fmt.Errorf("unrecognized image-references in release payload %q", releaseImage)
	}

	logger.Printf("Targeting release image %q for default external binaries", releaseImage)

	// Allow environmental overrides for individual component images.
	for _, tag := range is.Spec.Tags {
		componentEnvName := "EXTENSIONS_PAYLOAD_OVERRIDE_" + tag.Name
		componentOverrideImage := os.Getenv(componentEnvName)
		if len(componentOverrideImage) != 0 {
			tag.From.Name = componentOverrideImage
			logger.Printf("Overrode release image tag %q for with env %s value %q", tag.Name, componentEnvName, componentOverrideImage)
		}
	}

	return is, releaseImage, nil
}

func getRegistryAuthFilePath(logger *log.Logger, oc *util.CLI) (string, error) {
	// To extract binaries bearing external tests, we must inspect the release
	// payload under tests as well as extract content from component images
	// referenced by that payload.
	// openshift-tests is frequently run in the context of a CI job, within a pod.
	// CI sets $RELEASE_IMAGE_LATEST to a pullspec for the release payload under test. This
	// pull spec resolve to:
	// 1. A build farm ci-op-* namespace / imagestream location (anonymous access permitted).
	// 2. A quay.io/openshift-release-dev location (for tests against promoted ART payloads -- anonymous access permitted).
	// 3. A registry.ci.openshift.org/ocp-<arch>/release:<tag> (request registry.ci.openshift.org token).
	// Within the pod, we don't necessarily have a pull-secret for #3 OR the component images
	// a payload references (which are private, unless in a ci-op-* imagestream).
	// We try the following options:
	// 1. If set, use the REGISTRY_AUTH_FILE environment variable to an auths file with
	//    pull secrets capable of reading appropriate payload & component image
	//    information.
	// 2. If it exists, use a file /run/secrets/ci.openshift.io/cluster-profile/pull-secret
	//    (conventional location for pull-secret information for CI cluster profile).
	// 3. Use openshift-config secret/pull-secret from the cluster-under-test, if it exists
	//    (Microshift does not).
	// 4. Use unauthenticated access to the payload image and component images.
	registryAuthFilePath := os.Getenv("REGISTRY_AUTH_FILE")

	// if the environment variable is not set, extract the target cluster's
	// platform pull secret.
	if len(registryAuthFilePath) != 0 {
		logger.Printf("Using REGISTRY_AUTH_FILE environment variable: %v", registryAuthFilePath)
	} else {

		// See if the cluster-profile has stored a pull-secret at the conventional location.
		ciProfilePullSecretPath := "/run/secrets/ci.openshift.io/cluster-profile/pull-secret"
		_, err := os.Stat(ciProfilePullSecretPath)
		if !os.IsNotExist(err) {
			logger.Printf("Detected %v; using cluster profile for image access", ciProfilePullSecretPath)
			registryAuthFilePath = ciProfilePullSecretPath
		} else {
			// Inspect the cluster-under-test and read its cluster pull-secret dockerconfigjson value.
			clusterPullSecret, err := oc.AdminKubeClient().CoreV1().Secrets("openshift-config").Get(context.Background(), "pull-secret", metav1.GetOptions{})
			if err != nil {
				if kapierrs.IsNotFound(err) {
					logger.Printf("Cluster has no openshift-config secret/pull-secret; falling back to unauthenticated image access")
				} else {
					return "", fmt.Errorf("unable to read ephemeral cluster pull secret: %w", err)
				}
			} else {
				tmpDir, err := os.MkdirTemp("", "external-binary")
				clusterDockerConfig := clusterPullSecret.Data[".dockerconfigjson"]
				registryAuthFilePath = filepath.Join(tmpDir, ".dockerconfigjson")
				err = os.WriteFile(registryAuthFilePath, clusterDockerConfig, 0600)
				if err != nil {
					return "", fmt.Errorf("unable to serialize target cluster pull-secret locally: %w", err)
				}

				defer os.Remove(registryAuthFilePath)
				logger.Printf("Using target cluster pull-secrets for registry auth")
			}
		}
	}

	return registryAuthFilePath, nil
}

// createCachePath ensures the given path exists, is writable, and allows executing binaries.
func createCachePath(path string) error {
	// Create the directory if it doesn't exist.
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", path, err)
	}

	// Create a simple shell script to test executability.
	testFile := filepath.Join(path, "cache_test.sh")
	scriptContent := "#!/bin/sh\necho 'Executable test passed'"

	// Write the script to the cache directory.
	if err := os.WriteFile(testFile, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write test file in cache path %s: %w", path, err)
	}
	defer os.Remove(testFile)

	// Attempt to execute the test script.
	cmd := exec.Command(testFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute test file in cache path %s: %w", path, err)
	}

	// Check if the output is as expected.
	if string(output) != "Executable test passed\n" {
		return fmt.Errorf("unexpected output from executable test in cache path %s: %s", path, output)
	}

	return nil
}
