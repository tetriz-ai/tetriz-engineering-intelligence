// The wiring module carries every dependency the engine deliberately does not.
//
// The root module stays pure Go with no provider dependency, so `go get` and
// `go test ./...` work from a fresh clone with no C toolchain and no network.
// Everything that would break that lives here and is opted into:
//
//   - tetriz-connectors, so this module's types satisfy its host contract;
//   - tree-sitter, which is a C library, so a build needs a C compiler.
//
// The two replace directives resolve the engine and the connector contract from
// sibling clones, so this module builds against both in one working tree.
module github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/wiring

go 1.26.0

require (
	github.com/lib/pq v1.12.3
	github.com/stretchr/testify v1.12.1
	github.com/tetriz-ai/tetriz-connectors v0.0.0-00010101000000-000000000000
	github.com/tetriz-ai/tetriz-engineering-intelligence v0.0.0
)

require (
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

require (
	github.com/agnivade/levenshtein v1.2.2-0.20250519083737-420867539855 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.2 // indirect
	github.com/boyter/gocodewalker v1.5.2-0.20260227212453-19676720409f // indirect
	github.com/boyter/scc/v3 v3.7.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/danwakefield/fnmatch v0.0.0-20160403171240-cbb64ac3d964 // indirect
	github.com/go-enry/go-enry/v2 v2.9.6 // indirect
	github.com/go-enry/go-oniguruma v1.2.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/grpc v1.84.0 // indirect
)

// The grammars ship inside the module above at the version pinned here. They
// were later also published as modules of their own, and because Go resolves an
// import to the longest matching module path, those would shadow the in-module
// copies and make every grammar import ambiguous. Excluding them keeps one
// source for both the bindings and the grammars.
exclude (
	github.com/smacker/go-tree-sitter/golang v0.0.1
	github.com/smacker/go-tree-sitter/javascript v0.0.1
	github.com/smacker/go-tree-sitter/python v0.0.1
	github.com/smacker/go-tree-sitter/typescript v0.0.1
)

replace github.com/tetriz-ai/tetriz-engineering-intelligence => ../..

replace github.com/tetriz-ai/tetriz-connectors => ../../../tetriz-connectors
