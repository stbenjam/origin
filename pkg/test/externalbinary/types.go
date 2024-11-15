package externalbinary

type ExtensionTestSpecs []*ExtensionTestSpec

type ExtensionTestSpec struct { // todo convert to OTE
	Name   string
	Labels string
	Binary string
}
