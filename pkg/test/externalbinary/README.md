# External Binaries

This package includes the code used for working with external test binaries.
It's intended to house the implementation of the openshift-tests side of the
[openshift-tests extension interface](https://github.com/openshift/enhancements/pull/1676), which is only
partially implemented here for the moment.

There is a registry defined in binary.go, that lists the release image tag, and
path to each external test binary.  These binaries should implement the OTE
interface defined in the enhancement, and implemented by the vendorable
[openshift-tests-extension](https://github.com/openshift-eng/openshift-tests-extension).

## Requirements

If the architecture of your local system where `openshift-tests` will run
differs from the cluster under test, you should override the release payload
with a payload of the architecture of your own system, as it is where the
binaries will execute. Note, your OS must still be Linux. That means on Apple
Silicon, you'll still need to run this in a Linux environment, such as a
virtual machine, or x86 podman container.

### Development on Apple Silicon

1. Install and configure a podman machine.  You'll need to use more than the default RAM, at least 8GB:

```
$ podman machine init --cpus=4 --memory=8192
$ podman machine start
```

2. Start a development environment, using the multi-arch builder
   container:

```
podman run --platform linux/arm64 --authfile=$HOME/api-auth.json -it --rm -v $(pwd):/workspace -w /workspace --name openshift-tests registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-builder-multi-openshift-4.18 /bin/bash
```

3. Install oc

```
cd /tmp; wget https://mirror.openshift.com/pub/openshift-v4/arm64/clients/ocp-dev-preview/latest/openshift-client-linux-arm64-rhel9.tar.gz; tar xvf openshift-client-linux*.tar.gz; mv oc kubectl /usr/bin
```

2. Build openshift-tests:

```
$ make
[...]
Go compliance shim [499] [openshift-4.18][ci-openshift-golang-builder-latest.rhel9]: final command line arguments: "build" "-tags" "strictfipsruntime" "-trimpath" "-ldflags" "-X github.com/openshift/origin/pkg/version.versionFromGit=4.18.0-202411181637.p0.ge7558a4.assembly.stream.el9-e7558a4 -X github.com/openshift/origin/pkg/version.commitFromGit=e7558a4f6cd7019c668abfdde3be9195ee659c88 -X github.com/openshift/origin/pkg/version.gitTreeState=clean -X github.com/openshift/origin/pkg/version.buildDate=2024-11-20T12:54:49Z " "github.com/openshift/origin/cmd/openshift-tests"
Go compliance shim [499] [openshift-4.18][ci-openshift-golang-builder-latest.rhel9]: invoking real go binary
Go compliance shim [499] [openshift-4.18][ci-openshift-golang-builder-latest.rhel9]: Exited with: 0
[...]
$
```

3. You'll need a KUBECONFIG, and to configure the TEST_PROVIDER
   environment variable:

```
export TEST_PROVIDER='{"type":"aws"}'
export KUBECONFIG=/tmp/cluster-bot-2024-11-18-135036.kubeconfig
```

4. If not using an ARM64 cluster, you'll want to override the release
   payload for binary extraction to the ARM64 payload that closely
   matches the one the cluster is running:

```
export EXTENSIONS_PAYLOAD_OVERRIDE=registry.ci.openshift.org/ocp-arm64/release-arm64:4.18.0-0.nightly-arm64-2024-11-20-111707
```

4. Pare down the tests to just a couple, as we're likely not interested
   in running all 3365 tests. I want to grab 5 tests from an external
   binary (kube) and 5 from the internal suite:

```
$ ./openshift-tests run openshift/conformance/parallel --monitor event-collector --junit-dir=/tmp/junit --dry-run > all_tests.txt
$ cat all_tests.txt | egrep ^\" | sort -R | grep Suite:k8s | head -n5 >> rando.txt
$ cat all_tests.txt | egrep ^\" | sort -R | grep -v Suite:k8s | head -n5 >> rando.txt
```

5. Run the suite, filtering to only our random tests

```
$ ./openshift-tests run openshift/conformance/parallel --monitor event-collector --junit-dir=/tmp/junit --file rando.txt
```

6. Examine JUnit results in `/tmp/junit`

## Overrides

A number of environment variables for overriding the behavior of external
binaries are available, but in general this should "just work". A complex set
of logic for determining the optimal release payload, and which pull
credentials to use are found in this code, and extensively documented in code
comments.  The following environment variables are available to force certain
behaviors:

### Caching

By default, binaries will be cached in `$XDG_CACHE_HOME/openshift-tests`
(typically: `$HOME/.cache/openshift-tests`). Upon invocation, older binaries
than 7 days will be cleaned up. To disable this feature:

```bash
export OPENSHIFT_TESTS_DISABLE_CACHE=1
```

### Registry Auth Credentials

To change the pull secrets used for extracting the external binaries, set:

```bash
export REGISTRY_AUTH_FILE=$HOME/pull.json
```

### Release Payload

To change the payload used for extracting the external binaries, set:

```bash
export EXTENSIONS_PAYLOAD_OVERRIDE=registry.ci.openshift.org/ocp-arm64/release-arm64:4.18.0-0.nightly-arm64-2024-11-15-135718
```
