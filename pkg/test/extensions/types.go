package extensions

import (
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
)

// ExtensionInfo represents an extension to openshift-tests.
type ExtensionInfo struct {
	APIVersion string    `json:"apiVersion"`
	Source     Source    `json:"source"`
	Component  Component `json:"component"`

	// Suites that the extension wants to advertise/participate in.
	Suites []Suite `json:"suites"`

	// -- origin specific info --
	ExtensionArtifactDir string `json:"extension_artifact_dir"`
}

// Source contains the details of the commit and source URL.
type Source struct {
	// Commit from which this binary was compiled.
	Commit string `json:"commit"`
	// BuildDate ISO8601 string of when the binary was built
	BuildDate string `json:"build_date"`
	// GitTreeState lets you know the status of the git tree (clean/dirty)
	GitTreeState string `json:"git_tree_state"`
	// SourceURL contains the url of the git repository (if known) that this extension was built from.
	SourceURL string `json:"source_url,omitempty"`

	// -- origin specific info --

	// SourceImage contains the payload image it was extracted from.
	SourceImage string `json:"source_image,omitempty"`

	// SourceBinary contains the path in the source image for this extension.
	SourceBinary string `json:"source_binary,omitempty"`
}

// Component represents the component the binary acts on.
type Component struct {
	// The product this component is part of.
	Product string `json:"product"`
	// The type of the component.
	Kind string `json:"type"`
	// The name of the component.
	Name string `json:"name"`
}

// Suite represents additional suites the extension wants to advertise.
type Suite struct {
	// The name of the suite.
	Name string `json:"name"`
	// Parent suites this suite is part of.
	Parents []string `json:"parents,omitempty"`
	// Qualifiers are CEL expressions that are OR'd together for test selection that are members of the suite.
	Qualifiers []string `json:"qualifiers,omitempty"`
}

type Lifecycle string

var LifecycleInforming Lifecycle = "informing"
var LifecycleBlocking Lifecycle = "blocking"

// EnvironmentFlagName enumerates each possible EnvironmentFlag's name to be passed to the external binary
type EnvironmentFlagName string

const (
	platform             EnvironmentFlagName = "platform"
	network              EnvironmentFlagName = "network"
	networkStack         EnvironmentFlagName = "network-stack"
	upgrade              EnvironmentFlagName = "upgrade"
	topology             EnvironmentFlagName = "topology"
	architecture         EnvironmentFlagName = "architecture"
	externalConnectivity EnvironmentFlagName = "external-connectivity"
	optionalCapability   EnvironmentFlagName = "optional-capability"
	fact                 EnvironmentFlagName = "fact"
	version              EnvironmentFlagName = "version"
)

type EnvironmentFlagsBuilder struct {
	flags EnvironmentFlags
}

func (e *EnvironmentFlagsBuilder) AddPlatform(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(platform, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddNetwork(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(network, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddNetworkStack(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(networkStack, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddUpgrade(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(upgrade, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddTopology(value *configv1.TopologyMode) *EnvironmentFlagsBuilder {
	if value != nil {
		e.flags = append(e.flags, newEnvironmentFlag(topology, string(*value)))
	}
	return e
}

func (e *EnvironmentFlagsBuilder) AddArchitecture(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(architecture, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddExternalConnectivity(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(externalConnectivity, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddOptionalCapability(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(optionalCapability, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddFact(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(fact, value))
	return e
}

func (e *EnvironmentFlagsBuilder) AddVersion(value string) *EnvironmentFlagsBuilder {
	e.flags = append(e.flags, newEnvironmentFlag(version, value))
	return e
}

func (e *EnvironmentFlagsBuilder) Build() EnvironmentFlags {
	return e.flags
}

// EnvironmentFlag contains the info required to build an argument to pass to the external binary for the given Name
type EnvironmentFlag struct {
	Name         EnvironmentFlagName
	Value        string
	SinceVersion string
}

// newEnvironmentFlag creates an EnvironmentFlag including the determination of the SinceVersion
func newEnvironmentFlag(name EnvironmentFlagName, value string) EnvironmentFlag {
	return EnvironmentFlag{Name: name, Value: value, SinceVersion: EnvironmentFlagVersions[name]}
}

func (ef EnvironmentFlag) ArgString() string {
	return fmt.Sprintf("--%s=%s", ef.Name, ef.Value)
	//return fmt.Sprintf("%s=%s", ef.Name, ef.Value)
}

type EnvironmentFlags []EnvironmentFlag

// ArgStrings properly formats all EnvironmentFlags as a list of argument strings to pass to the external binary
func (ef EnvironmentFlags) ArgStrings() []string {
	var argStrings []string
	for _, flag := range ef {
		argStrings = append(argStrings, flag.ArgString())
	}
	return argStrings
}

func (ef EnvironmentFlags) String() string {
	return strings.Join(ef.ArgStrings(), ", ")
}

// EnvironmentFlagVersions holds the "Since" version metadata for each flag.
var EnvironmentFlagVersions = map[EnvironmentFlagName]string{
	platform:             "v1.0",
	network:              "v1.0",
	networkStack:         "v1.0",
	upgrade:              "v1.0",
	topology:             "v1.0",
	architecture:         "v1.0",
	externalConnectivity: "v1.0",
	optionalCapability:   "v1.0",
	fact:                 "v1.0", //TODO(sgoeddel): this will be set in a later version
	version:              "v1.0",
}
