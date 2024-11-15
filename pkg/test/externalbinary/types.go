package externalbinary

import "k8s.io/apimachinery/pkg/util/sets"

type ExtensionTestSpecs []*ExtensionTestSpec

type Lifecycle string

var LifecycleInforming Lifecycle = "informing"
var LifecycleBlocking Lifecycle = "blocking"

type ExtensionTestSpec struct {
	Name string `json:"name"`

	// OriginalName contains the very first name this test was ever known as, used to preserve
	// history across all names.
	OriginalName string `json:"originalName,omitempty"`

	// Labels are single string values to apply to the test spec
	Labels sets.Set[string] `json:"labels"`

	// Tags are key:value pairs
	Tags map[string]string `json:"tags,omitempty"`

	// Resources gives optional information about what's required to run this test.
	Resources Resources `json:"resources"`

	// Source is the origin of the test.
	Source string `json:"source"`

	// Lifecycle informs the executor whether the test is informing only, and should not cause the
	// overall job run to fail, or if it's blocking where a failure of the test is fatal.
	// Informing lifecycle tests can be used temporarily to gather information about a test's stability.
	// Tests must not remain informing forever.
	Lifecycle Lifecycle `json:"lifecycle"`

	Binary string
}

type Resources struct {
	Isolation Isolation `json:"isolation"`
	Memory    string    `json:"memory,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	Timeout   string    `json:"timeout,omitempty"`
}

type Isolation struct {
	Mode     string   `json:"mode,omitempty"`
	Conflict []string `json:"conflict,omitempty"`
}
