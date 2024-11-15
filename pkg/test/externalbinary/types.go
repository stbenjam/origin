package externalbinary

type ExtensionTestSpecs []*ExtensionTestSpec

type ExtensionTestSpec struct {
	Name   string
	Labels string
	Binary string
}
