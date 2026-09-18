# Accumulator Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `terraform-provider-accumulator`, a protocol-6 OpenTofu provider with two resources (`accumulator_list`, `accumulator_set`) that accumulate inputs across applies into state, plus import, generated docs, and the OpenTofu-first CI and release pipeline.

**Architecture:** `internal/accumulate` holds all list/set behavior as pure Go with zero Terraform imports, so it is unit-testable without `TF_ACC`. `internal/provider` is a thin translation between `types.List`/`types.Set` values and those pure functions: two resources, one shared value-conversion file, and empty `Read`/`Delete` methods because state is the only store. No `ModifyPlan`, no data sources, no provider-defined functions, no provider configuration.

**Tech Stack:** Go 1.25.12 (`terraform-plugin-framework` v1.19.0, `terraform-plugin-framework-validators` v0.19.0, `terraform-plugin-go` v0.31.0, `terraform-plugin-testing` v1.16.0, `terraform-plugin-docs` v0.25.0 in a separate `tools/` module), protocol 6, goreleaser v2, GitHub Actions driving OpenTofu only.

**Spec:** `docs/superpowers/specs/2026-09-17-terraform-provider-accumulator-design.md` (approved). Read it alongside this plan; the plan argues from it.

**Reference implementation:** the sibling `../terraform-provider-pki` repository. Several tasks adapt files from it verbatim. Where a step says "adapt from pki", read the named pki file first.

## Global Constraints

Every task's requirements implicitly include this section.

- **Module path** `github.com/nijave/terraform-provider-accumulator`. **Provider address** `registry.opentofu.org/nijave/accumulator`. **Short name** in configuration and in the test factory: `accumulator`.
- **Go directive `go 1.25.12`.** The build toolchain in this environment is Go 1.26.8. After `go mod init` writes a higher directive, run `go mod edit -go=1.25.12`. `terraform-plugin-testing` v1.16.0 requires `go 1.25.8`, so `1.25.12` is above the floor.
- **OpenTofu is the primary target.** Support floor OpenTofu >= 1.10. Terraform >= 1.10 is expected to work but is not tested and not the reference platform. **No acceptance test needs a `tfversion` check**; CI never runs below 1.10.
- **Protocol 6.** `terraform-registry-manifest.json` declares `"protocol_versions": ["6.0"]`.
- **License GPL-3.0-or-later.** Every `.go` file starts with `// SPDX-License-Identifier: GPL-3.0-or-later`. Nothing under BUSL-1.1 may be linked or downloaded; CI installs OpenTofu, never Terraform.
- **`internal/accumulate` stays Terraform-free.** Never add a `terraform-plugin-*` or `github.com/opentofu/*` import under `internal/accumulate`; Task 2's boundary test fails the build if you do. Translation belongs in `internal/provider`.
- **`outputs` has no `UseStateForUnknown`.** It must plan as unknown whenever an input changes. `id` does carry `UseStateForUnknown`.
- **No `ModifyPlan`.** Spec section 14 rejects it. The framework only marks computed null-config attributes unknown when the proposed new state differs from the prior state, so a no-change refresh keeps prior state and the plan is empty without a plan modifier. Do not add one.
- **`docs/` is generated, never hand-edited.** `make docs` must leave the tree clean and CI fails on a resulting `git diff`. `docs/superpowers/` is hand-written, is never an input or output of generation, and must not be clobbered. `tools/schema.json` is gitignored so exporting the schema is not mistaken for drift.
- **Formatter/linter/test runner:** `gofmt -l .` empty, `go vet ./...` clean, `go test ./internal/...` green, `make testacc` green against `tofu`.
- **Every `ExpectError` regexp must match text only the provider produces.** OpenTofu echoes the offending configuration inside a diagnostic, so a pattern matching an attribute name or literal that appears in the test's own config passes whether or not the provider ran. The patterns in Task 9 (`must be a JSON object`, `not recognized`, `must be an array of strings`) are provider-only phrases and the configs deliberately do not contain them.
- **Conventional Commits. Stage explicit paths;** never `git add -A`.

## File Structure

| File | Responsibility | Task |
| --- | --- | --- |
| `go.mod`, `go.sum` | Root module | 1 |
| `LICENSE` | GPL-3.0 text, copied from pki | 1 |
| `.gitignore` | Build output, `tools/schema.json`, agent scratch | 1 |
| `terraform-registry-manifest.json` | Protocol 6 declaration | 1 |
| `main.go` | `providerserver.Serve`, ldflags version/commit | 1 |
| `internal/provider/provider.go` | Provider metadata, empty schema, resource registration | 1, 7, 8 |
| `internal/provider/provider_test.go` | Factories, `testAccPreCheck`, schema guard, SPDX walk, em-dash guard, shared test helpers | 1, 7 |
| `tools/go.mod`, `tools/tools.go` | `tfplugindocs`, isolated from the provider module | 1 |
| `GNUmakefile` | `build`, `test`, `testacc`, `fmt`, `vet`, `release`; `docs` added in Task 10 | 1, 10 |
| `internal/accumulate/list.go` | `Trim`, `Append` | 2 |
| `internal/accumulate/boundary_test.go` | Fails if the package imports Terraform | 2 |
| `internal/accumulate/set.go` | `Merge` | 3 |
| `internal/accumulate/id.go` | `HashID` | 4 |
| `internal/accumulate/import.go` | `ImportSeed`, `ParseImport` | 5 |
| `internal/provider/model.go` | `types.List`/`types.Set` <-> `[]string` conversion | 6 |
| `internal/provider/resource_list.go` | `accumulator_list` | 7, 9 |
| `internal/provider/resource_list_test.go` | List acceptance tests | 7 |
| `internal/provider/resource_set.go` | `accumulator_set` | 8, 9 |
| `internal/provider/resource_set_test.go` | Set acceptance tests | 8 |
| `internal/provider/import_test.go` | Import acceptance tests for both resources | 9 |
| `templates/index.md.tmpl` | Source for `docs/index.md` | 10 |
| `examples/**` | Provider and resource examples, `import.sh` | 10 |
| `tools/gen-schema.sh` | Exports the schema with `tofu` for `tfplugindocs` | 10 |
| `docs/index.md`, `docs/resources/*.md` | Generated documentation, committed | 10 |
| `.github/workflows/test.yml` | build, unit, generate, acceptance, license jobs | 11 |
| `.github/workflows/release.yml` | goreleaser on `v*` tags | 11 |
| `.github/dependabot.yml` | Actions, root module, `tools` module | 11 |
| `.goreleaser.yml` | Release build | 11 |
| `README.md` | Human-facing entry point | 11 |

---

### Task 1: Module, provider skeleton, and acceptance-test harness

An empty but servable provider plus the plumbing every later task needs. The deliverable is a module that builds, vets, passes `go test ./internal/...`, and plans successfully against a locally built binary.

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `LICENSE`
- Create: `.gitignore`
- Create: `terraform-registry-manifest.json`
- Create: `main.go`
- Create: `internal/provider/provider.go`
- Test: `internal/provider/provider_test.go`
- Create: `tools/go.mod`, `tools/go.sum`, `tools/tools.go`
- Modify: `GNUmakefile`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func New(version string) func() provider.Provider`
  - `type accumulatorProvider struct { version string }` implementing `provider.Provider`
  - `var testAccProtoV6ProviderFactories map[string]func() (tfprotov6.ProviderServer, error)` (test-only)
  - `func testAccPreCheck(t *testing.T)` (test-only)

- [ ] **Step 1: Initialise the module and add the framework dependencies**

```bash
go mod init github.com/nijave/terraform-provider-accumulator
go get github.com/hashicorp/terraform-plugin-framework@v1.19.0
go get github.com/hashicorp/terraform-plugin-framework-validators@v0.19.0
go get github.com/hashicorp/terraform-plugin-go@v0.31.0
go get github.com/hashicorp/terraform-plugin-testing@v1.16.0
go mod edit -go=1.25.12
go mod tidy
```

All four libraries are MPL-2.0, which is GPLv3-compatible (spec section 13). Do not grep their `LICENSE` files for "Incompatible With Secondary Licenses": that phrase appears in MPL-2.0's own boilerplate whether or not it is applied, so the grep is a false positive. The real signal is whether source files carry the Exhibit B notice, and they do not.

- [ ] **Step 2: Copy the license**

```bash
cp ../terraform-provider-pki/LICENSE LICENSE
head -2 LICENSE
```

Expected: `GNU GENERAL PUBLIC LICENSE` / `Version 3, 29 June 2007`. If the sibling checkout is absent, obtain the standard GPL-3.0 text another way and verify the same two lines.

- [ ] **Step 3: Write `.gitignore`**

```gitignore
# If you prefer the allow list template instead of the deny list, see community template:
# https://github.com/github/gitignore/blob/main/community/Golang/Go.AllowList.gitignore
#
# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary, built with `go test -c`
*.test

# Code coverage profiles and other test artifacts
*.out
coverage.*
*.coverprofile
profile.cov

# Dependency directories (remove the comment below to include it)
# vendor/

# Go workspace file
go.work
go.work.sum

# env file
.env

# Editor/IDE
# .idea/
# .vscode/

# Agent scratch: git worktrees and subagent-driven-development ledgers
.claude/worktrees/
.superpowers/

# Build output
dist/

# Provider schema exported by tools/gen-schema.sh for `make docs`
tools/schema.json
```

- [ ] **Step 4: Write `terraform-registry-manifest.json`**

```json
{
    "version": 1,
    "metadata": {
        "protocol_versions": ["6.0"]
    }
}
```

- [ ] **Step 5: Write `main.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/nijave/terraform-provider-accumulator/internal/provider"
)

// Documentation is generated from the tools module, which keeps tfplugindocs'
// dependency graph out of this module's go.sum. There is deliberately no
// //go:generate directive here: `go generate ./...` at the repo root does not
// descend into a nested module. `make docs` is the entry point, and CI runs it
// followed by a git diff.

var (
	// version and commit are set by goreleaser's ldflags.
	version = "dev"
	commit  = ""
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	_ = commit // recorded in the binary for support purposes

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		// The OpenTofu registry is the primary distribution channel. This
		// address only affects dev overrides and the reattach output in debug
		// mode; the same binary serves either registry.
		Address: "registry.opentofu.org/nijave/accumulator",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
```

- [ ] **Step 6: Write `internal/provider/provider.go`**

There are no data sources and no provider-defined functions (spec section 2), so the provider implements `provider.Provider` only, not `provider.ProviderWithFunctions`.

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Ensure accumulatorProvider satisfies the interface the framework dispatches
// on. If a method signature drifts, this fails at compile time rather than at
// plan time.
var _ provider.Provider = (*accumulatorProvider)(nil)

// accumulatorProvider keeps accumulated history in Terraform state. There is no
// endpoint, no credential, and no client.
type accumulatorProvider struct {
	// version is injected by main.go from goreleaser's ldflags.
	version string
}

// New returns a provider factory for the given version string.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &accumulatorProvider{version: version}
	}
}

func (p *accumulatorProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "accumulator"
	resp.Version = p.version
}

// Schema declares no attributes.
//
// There is nothing to configure: state is the only store, so there is no
// endpoint, no credential, and no client to build.
func (p *accumulatorProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resources that accumulate a value over successive applies while keeping a " +
			"fixed history length. The accumulated history lives in Terraform state, so there is no " +
			"external store, endpoint, or credential to stand up.",
	}
}

// Configure is a no-op. There is no client to build and nothing to validate, so
// nothing is passed down to the resources.
func (p *accumulatorProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *accumulatorProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

// DataSources returns nothing. There is nothing to query that a resource does
// not already expose as a state attribute, so the provider has no data sources.
func (p *accumulatorProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
```

- [ ] **Step 7: Write the acceptance-test harness**

`internal/provider/provider_test.go`. Using package `provider_test` is deliberate: it forces the tests to exercise only the exported surface, the same way OpenTofu does.

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/nijave/terraform-provider-accumulator/internal/provider"
)

// testAccProtoV6ProviderFactories serves the provider in-process over protocol
// 6. Every acceptance test uses this; there is no external provider to install
// and no registry lookup.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"accumulator": providerserver.NewProtocol6WithError(provider.New("test")()),
}

// testAccPreCheck fails fast with an actionable message when the harness is
// misconfigured, rather than letting terraform-plugin-testing download a
// Terraform binary. There are no credentials to check: every resource in this
// provider is self-contained.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set; skipping the acceptance test")
	}
	path := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if path == "" {
		t.Fatal("TF_ACC_TERRAFORM_PATH is not set. Run `make testacc`, which points it at the tofu binary. " +
			"Without it the harness falls back to downloading Terraform, which is not the tested platform.")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("TF_ACC_TERRAFORM_PATH=%q is not usable: %v", path, err)
	}
	// Without this, terraform-plugin-testing pairs its default host
	// registry.terraform.io with the legacy "-" namespace it registers for
	// reattach, and OpenTofu refuses the combination with a message about
	// provider address parsing that says nothing about the real cause. Fail
	// here instead, where the message can name the fix.
	if got := os.Getenv("TF_ACC_PROVIDER_HOST"); got != "registry.opentofu.org" {
		t.Fatalf("TF_ACC_PROVIDER_HOST is %q, want \"registry.opentofu.org\". Run `make testacc`, which "+
			"sets it. Without it terraform-plugin-testing pairs its default registry.terraform.io host "+
			"with the legacy \"-\" namespace, and OpenTofu rejects that pairing before the provider is "+
			"ever reached.", got)
	}
}

// TestProviderSchema is a unit test -- no TF_ACC required -- that catches a
// malformed schema at `go test` time instead of at `tofu plan` time. Every
// resource added in a later task is validated by it automatically, because it
// walks whatever the provider registers. Task 8 extends the config to plan one
// of each resource.
func TestProviderSchema(t *testing.T) {
	t.Parallel()
	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:   `provider "accumulator" {}`,
			PlanOnly: true,
		}},
	})
}

// TestUserFacingStringsUseEmDashes pins the house style: Go comments in this
// repository write a parenthetical break as `--`, but user-facing text renders
// it, so it has to be a real em dash there. `--` inside a MarkdownDescription
// reaches the registry documentation as two literal hyphens.
//
// Scanning string literals rather than raw file text separates the two cases
// cleanly: a comment is not a literal. A literal is checked as its concatenated
// value, not one BasicLit at a time, because this package wraps long
// descriptions across `+`-joined literals and a per-literal scan would miss a
// `--` split across the join.
func TestUserFacingStringsUseEmDashes(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package source: %v", err)
	}

	// constString reports the value of a compile-time constant string
	// expression: a string literal, or a `+` concatenation of them. It returns
	// false for anything it cannot fully resolve, so the caller can keep
	// walking rather than guess.
	var constString func(ast.Node) (string, bool)
	constString = func(n ast.Node) (string, bool) {
		switch e := n.(type) {
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				return "", false
			}
			v, err := strconv.Unquote(e.Value)
			if err != nil {
				return "", false
			}
			return v, true
		case *ast.BinaryExpr:
			if e.Op != token.ADD {
				return "", false
			}
			left, ok := constString(e.X)
			if !ok {
				return "", false
			}
			right, ok := constString(e.Y)
			if !ok {
				return "", false
			}
			return left + right, true
		default:
			return "", false
		}
	}

	checked := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				value, ok := constString(n)
				if !ok {
					return true
				}
				checked++
				// A concatenation is checked here as a whole, and its child
				// literals must not be re-checked on their own: a `--` split
				// across the join would be reported twice. Returning false
				// stops the descent once a node has been folded.
				if strings.Contains(value, "--") {
					t.Errorf("%s: user-facing string contains \"--\" (%q); "+
						"it renders as two literal hyphens, so use an em dash instead",
						fset.Position(n.Pos()), value)
				}
				return false
			})
		}
	}
	if checked == 0 {
		t.Fatal("no string literals were checked; the test is not doing what it claims")
	}
}

// TestEveryGoFileHasTheSPDXHeader guards the whole module, not just this
// package. Every task adds files under main.go, internal/, and tools/, and this
// is the only check that sees all of them.
func TestEveryGoFileHasTheSPDXHeader(t *testing.T) {
	t.Parallel()

	const root = "../.."
	const want = "// SPDX-License-Identifier: GPL-3.0-or-later"
	skipDirs := map[string]bool{
		".git":         true,
		".claude":      true,
		".superpowers": true,
	}

	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			return nil
		}
		checked++
		if !strings.HasPrefix(string(content), want) {
			t.Errorf("%s does not start with %q", path, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if checked == 0 {
		t.Fatal("no Go files were checked; the test is not doing what it claims")
	}
}
```

- [ ] **Step 8: Set up the tools module**

```bash
mkdir -p tools && cd tools && go mod init tools && go get github.com/hashicorp/terraform-plugin-docs@v0.25.0 && go mod edit -go=1.25.12 && cd ..
```

`tools/tools.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build generate

package tools

import (
	// Documentation generation.
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. --provider-name accumulator --providers-schema schema.json
```

The build tag is `generate`, not `tools`. `--providers-schema schema.json` is produced by `tools/gen-schema.sh`, added in Task 10; `make docs` is not runnable until then.

- [ ] **Step 9: Write the `GNUmakefile`**

The `docs` target is added in Task 10 (its script does not exist yet).

```makefile
default: test

.PHONY: build
build:
	go build -o dist/ ./...

.PHONY: test
test:
	go test ./... -timeout 10m

# TF_ACC_PROVIDER_HOST pins the reattach/required_providers synthesis in
# terraform-plugin-testing to registry.opentofu.org. Without it, the library
# defaults to registry.terraform.io while still registering the legacy "-"
# namespace as a reattach candidate, and OpenTofu's provider address parser
# rejects that pairing outright, failing every acceptance test before it
# reaches the provider.
.PHONY: testacc
testacc:
	@command -v tofu >/dev/null || (echo "tofu not found in PATH; OpenTofu >= 1.10 is required" && exit 1)
	TF_ACC=1 TF_ACC_TERRAFORM_PATH="$$(command -v tofu)" TF_ACC_PROVIDER_HOST=registry.opentofu.org go test ./... -v $(TESTARGS) -timeout 120m

.PHONY: fmt
fmt:
	gofmt -w -l .

.PHONY: vet
vet:
	go vet ./...

.PHONY: release
release:
	@test $${RELEASE_VERSION?Please set environment variable RELEASE_VERSION}
	@git tag $$RELEASE_VERSION
	@git push origin $$RELEASE_VERSION
```

- [ ] **Step 10: Verify the module and the provider binary**

```bash
gofmt -l . && go vet ./... && go build -v ./... && go test ./internal/... -count=1
```

Expected: no output from `gofmt -l`, and `ok` for `internal/provider`.

Then confirm a real plan succeeds against a locally built binary:

```bash
mkdir -p /tmp/accumulator-devcheck && go build -o /tmp/accumulator-devcheck/terraform-provider-accumulator .
cat > /tmp/accumulator-devcheck/dev.tfrc <<EOF
provider_installation {
  dev_overrides {
    "nijave/accumulator" = "/tmp/accumulator-devcheck"
  }
  direct {}
}
EOF
cat > /tmp/accumulator-devcheck/main.tf <<'EOF'
terraform {
  required_providers {
    accumulator = { source = "nijave/accumulator" }
  }
}
provider "accumulator" {}
EOF
TF_CLI_CONFIG_FILE=/tmp/accumulator-devcheck/dev.tfrc tofu -chdir=/tmp/accumulator-devcheck plan
```

Expected: "No changes" plus the expected dev-override warning. A protocol mismatch or schema error surfaces here. Keep the scratch directory; later tasks reuse it for manual checks. Nothing in it is committed.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum LICENSE .gitignore terraform-registry-manifest.json main.go internal/provider/provider.go internal/provider/provider_test.go tools/go.mod tools/go.sum tools/tools.go GNUmakefile
git commit -m "feat: provider skeleton on terraform-plugin-framework with protocol 6"
```

---

### Task 2: `internal/accumulate` list behavior and the boundary guard

`Trim` and `Append` are the whole of `accumulator_list`'s arithmetic. The boundary test is written here, with the package's first file, so every later file in the package is guarded automatically.

**Files:**
- Create: `internal/accumulate/list.go`
- Test: `internal/accumulate/list_test.go`
- Test: `internal/accumulate/boundary_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces:
  - `func Trim(values []string, length int) []string`
  - `func Append(outputs, inputs []string, length int) []string`
  - `func TestPackageImportsNoTerraform(t *testing.T)` (test-only guard)

- [ ] **Step 1: Write the failing unit tests**

`internal/accumulate/list_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
)

func TestTrim(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		length int
		want   []string
	}{
		"nil values, positive length": {nil, 3, []string{}},
		"empty values":                {[]string{}, 3, []string{}},
		"zero length":                 {[]string{"a", "b"}, 0, []string{}},
		"negative length":             {[]string{"a", "b"}, -1, []string{}},
		"length exceeds values":       {[]string{"a", "b"}, 5, []string{"a", "b"}},
		"length equals values":        {[]string{"a", "b"}, 2, []string{"a", "b"}},
		"keeps the last elements":     {[]string{"a", "b", "c"}, 2, []string{"b", "c"}},
		"length one keeps the last":   {[]string{"a", "b", "c"}, 1, []string{"c"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Trim(tc.values, tc.length)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Trim(%v, %d) = %v, want %v", tc.values, tc.length, got, tc.want)
			}
			if got == nil {
				t.Fatal("Trim returned nil; it must always return a non-nil slice")
			}
		})
	}
}

// TestTrimDoesNotAliasInput guards the slices the provider writes back to
// state: a later append must not mutate the caller's slice.
func TestTrimDoesNotAliasInput(t *testing.T) {
	t.Parallel()
	values := []string{"a", "b", "c"}
	got := Trim(values, 2)
	got[0] = "changed"
	if values[1] != "b" {
		t.Fatalf("Trim aliased its input: values[1] = %q, want \"b\"", values[1])
	}
}

func TestAppend(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs []string
		length          int
		want            []string
	}{
		"create seeds from inputs":       {nil, []string{"a"}, 1, []string{"a"}},
		"append within length":           {[]string{"a"}, []string{"b"}, 2, []string{"a", "b"}},
		"append trims oldest":            {[]string{"a", "b"}, []string{"c"}, 2, []string{"b", "c"}},
		"append to empty history":        {[]string{}, []string{"a"}, 2, []string{"a"}},
		"inputs truncated on create":     {nil, []string{"a", "b"}, 1, []string{"b"}},
		"whole new list is appended":     {[]string{"a"}, []string{"a", "b"}, 5, []string{"a", "a", "b"}},
		"zero length discards everything": {[]string{"a"}, []string{"b"}, 0, []string{}},
		"empty inputs keeps history":     {[]string{"a"}, []string{}, 1, []string{"a"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Append(tc.outputs, tc.inputs, tc.length)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Append(%v, %v, %d) = %v, want %v", tc.outputs, tc.inputs, tc.length, got, tc.want)
			}
			if got == nil {
				t.Fatal("Append returned nil; it must always return a non-nil slice")
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/accumulate/ -run 'TestTrim|TestAppend' -v
```

Expected: FAIL with "undefined: Trim" and "undefined: Append".

- [ ] **Step 3: Write `internal/accumulate/list.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

// Package accumulate holds the list and set behavior of the accumulator
// resources as pure Go. It imports no Terraform packages, so every decision it
// makes is unit-testable without a plugin harness and internal/provider stays a
// mechanical translation.
package accumulate

// Trim returns the last length elements of values. A length <= 0 yields an
// empty slice; a length >= len(values) yields the whole slice. A nil or empty
// input yields an empty (non-nil) slice, so the value written back to state is
// always concrete and downstream JSON is "[]" rather than null.
//
// The result never aliases values, so a caller can append to it without
// mutating the slice it read from state.
func Trim(values []string, length int) []string {
	if length <= 0 || len(values) == 0 {
		return []string{}
	}
	if length >= len(values) {
		return append([]string{}, values...)
	}
	return append([]string{}, values[len(values)-length:]...)
}

// Append returns Trim(outputs ++ inputs, length).
//
// There is deliberately no deduplication: re-submitting a value appends it
// again. Only accumulator_set deduplicates.
func Append(outputs, inputs []string, length int) []string {
	combined := make([]string, 0, len(outputs)+len(inputs))
	combined = append(combined, outputs...)
	combined = append(combined, inputs...)
	return Trim(combined, length)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/accumulate/ -run 'TestTrim|TestAppend' -v
```

Expected: PASS.

- [ ] **Step 5: Write the boundary test**

`internal/accumulate/boundary_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImportsNoTerraform enforces the boundary spec section 3 draws:
// internal/accumulate is pure Go and imports zero Terraform packages, so every
// list and set decision is testable without a plugin harness and the framework
// layer stays a mechanical translation.
//
// This is a test rather than a convention because the pressure to violate it
// is real and arrives gradually: one diag.Diagnostics here, one types.List
// there, and each individual step looks harmless.
func TestPackageImportsNoTerraform(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fset := token.NewFileSet()
	forbidden := []string{
		// The dash matters: it distinguishes the modern, split-out
		// terraform-plugin-* module family from the legacy pre-split in-tree
		// SDK path github.com/hashicorp/terraform/helper/schema. Both prefixes
		// are required; do not collapse this to one entry.
		"github.com/hashicorp/terraform-",
		"github.com/hashicorp/terraform/",
		"github.com/opentofu/",
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parsing %s: %v", e.Name(), err)
			continue
		}
		checked++
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if strings.HasPrefix(path, bad) {
					t.Errorf("%s imports %q; internal/accumulate must not depend on Terraform packages", e.Name(), path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Go files were checked; the test is not doing what it claims")
	}
}
```

- [ ] **Step 6: Run the guard and the full unit suite**

```bash
go test ./internal/accumulate/ -v
```

Expected: PASS for `TestTrim`, `TestTrimDoesNotAliasInput`, `TestAppend`, and `TestPackageImportsNoTerraform`.

- [ ] **Step 7: Commit**

```bash
git add internal/accumulate/list.go internal/accumulate/list_test.go internal/accumulate/boundary_test.go
git commit -m "feat: accumulate list trimming and appending with a Terraform-free boundary guard"
```

---

### Task 3: `internal/accumulate` set union

**Files:**
- Create: `internal/accumulate/set.go`
- Test: `internal/accumulate/set_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces: `func Merge(outputs, inputs []string) []string`

- [ ] **Step 1: Write the failing unit test**

`internal/accumulate/set_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		outputs, inputs []string
		want            []string
	}{
		"nil and nil":             {nil, nil, []string{}},
		"nil and empty":           {nil, []string{}, []string{}},
		"first union":             {[]string{"a"}, []string{"b"}, []string{"a", "b"}},
		"idempotent on overlap":   {[]string{"a", "b"}, []string{"b", "a"}, []string{"a", "b"}},
		"duplicates within inputs": {[]string{}, []string{"a", "a", "b"}, []string{"a", "b"}},
		"order is first occurrence": {[]string{"c", "a"}, []string{"b", "a"}, []string{"c", "a", "b"}},
		"empty inputs":            {[]string{"a"}, []string{}, []string{"a"}},
		"empty outputs":           {[]string{}, []string{"a"}, []string{"a"}},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got := Merge(tc.outputs, tc.inputs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Merge(%v, %v) = %v, want %v", tc.outputs, tc.inputs, got, tc.want)
			}
			if got == nil {
				t.Fatal("Merge returned nil; it must always return a non-nil slice")
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/accumulate/ -run TestMerge -v
```

Expected: FAIL with "undefined: Merge".

- [ ] **Step 3: Write `internal/accumulate/set.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

// Merge returns the union of outputs and inputs, first occurrence wins, with a
// stable order so the value written to state is deterministic. A nil or empty
// input yields an empty (non-nil) slice.
//
// It is idempotent: applying it whenever the resource is updated is correct
// whether or not the inputs changed, which removes the need for the resource to
// compare planned inputs against state.
func Merge(outputs, inputs []string) []string {
	merged := make([]string, 0, len(outputs)+len(inputs))
	seen := make(map[string]struct{}, len(outputs)+len(inputs))

	for _, group := range [2][]string{outputs, inputs} {
		for _, v := range group {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			merged = append(merged, v)
		}
	}
	return merged
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/accumulate/ -run TestMerge -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/accumulate/set.go internal/accumulate/set_test.go
git commit -m "feat: accumulate set union with stable ordering"
```

---

### Task 4: `internal/accumulate` resource identity

`id` is the lowercase hex SHA-256 of the canonical JSON encoding of the inputs list present at creation. It is informational: Terraform keys state by resource address, not by `id`.

**Files:**
- Create: `internal/accumulate/id.go`
- Test: `internal/accumulate/id_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces: `func HashID(values []string) string`

- [ ] **Step 1: Write the failing unit test**

The expected values are `sha256` of the exact JSON bytes, computed independently of the implementation.

`internal/accumulate/id_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import "testing"

func TestHashID(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		want   string
	}{
		"nil normalizes to an empty list": {
			nil,
			"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", // sha256("[]")
		},
		"empty slice": {
			[]string{},
			"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", // sha256("[]")
		},
		"one element": {
			[]string{"a"},
			"0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002", // sha256(`["a"]`)
		},
		"two elements": {
			[]string{"a", "b"},
			"0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13", // sha256(`["a","b"]`)
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			if got := HashID(tc.values); got != tc.want {
				t.Fatalf("HashID(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// TestHashIDOrderMatters pins that the hash is over the exact list, not over a
// sorted or set-like view of it. accumulator_list treats a reordered list as a
// new value, so the identifier that records the creation inputs must too.
func TestHashIDOrderMatters(t *testing.T) {
	t.Parallel()
	if HashID([]string{"a", "b"}) == HashID([]string{"b", "a"}) {
		t.Fatal("HashID is order-insensitive; it must hash the list as written")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/accumulate/ -run TestHashID -v
```

Expected: FAIL with "undefined: HashID".

- [ ] **Step 3: Write `internal/accumulate/id.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// HashID returns the lowercase hex SHA-256 of the canonical JSON encoding of
// values. A nil slice is normalized to an empty one first, so an empty inputs
// list hashes "[]" rather than JSON null.
//
// JSON encoding makes the identifier deterministic: recreating a resource with
// the same initial inputs yields the same id. It is informational only, since
// Terraform keys state by resource address.
func HashID(values []string) string {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		// json.Marshal of a []string has no error path. Keeping the branch means
		// a future change to the input type cannot silently hash empty bytes.
		panic(fmt.Sprintf("accumulate: hashing %T: %v", values, err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/accumulate/ -run TestHashID -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/accumulate/id.go internal/accumulate/id_test.go
git commit -m "feat: deterministic JSON-based resource identifier"
```

---

### Task 5: `internal/accumulate` import seed parsing

The import ID is a JSON object whose values are arrays of strings. It is the only way to seed a resource, because there is no external system to discover one from.

**Files:**
- Create: `internal/accumulate/import.go`
- Test: `internal/accumulate/import_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces:
  - `type ImportSeed struct { Inputs []string; Outputs []string }`
  - `func ParseImport(id string) (ImportSeed, error)`

- [ ] **Step 1: Write the failing unit tests**

`internal/accumulate/import_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseImport(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id      string
		want    ImportSeed
	}{
		"list shape": {
			`{"inputs":["a","b"],"outputs":["a","b","c"]}`,
			ImportSeed{Inputs: []string{"a", "b"}, Outputs: []string{"a", "b", "c"}},
		},
		"set shape": {
			`{"outputs":["a","b"]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{"a", "b"}},
		},
		"empty object": {
			`{}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{}},
		},
		"missing inputs": {
			`{"outputs":["a"]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{"a"}},
		},
		"missing outputs": {
			`{"inputs":["a"]}`,
			ImportSeed{Inputs: []string{"a"}, Outputs: []string{}},
		},
		"empty arrays stay empty": {
			`{"inputs":[],"outputs":[]}`,
			ImportSeed{Inputs: []string{}, Outputs: []string{}},
		},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, err := ParseImport(tc.id)
			if err != nil {
				t.Fatalf("ParseImport(%q): %v", tc.id, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseImport(%q) = %+v, want %+v", tc.id, got, tc.want)
			}
			if got.Inputs == nil || got.Outputs == nil {
				t.Fatal("ParseImport returned a nil slice; a missing key must mean empty, not null")
			}
		})
	}
}

func TestParseImportRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		id     string
		substr string
	}{
		"invalid json":          {`not json`, "must be a JSON object"},
		"bare array":            {`["a"]`, "must be a JSON object"},
		"bare scalar":           {`"a"`, "must be a JSON object"},
		"json null":             {`null`, "must be a JSON object"},
		"empty id":              {``, "must be a JSON object"},
		"unknown key":           {`{"outupts":["a"]}`, "not recognized"},
		"unknown key is named":  {`{"outupts":["a"]}`, `"outupts"`},
		"inputs not an array":   {`{"inputs":"a"}`, "must be an array of strings"},
		"inputs null":           {`{"inputs":null}`, "must be an array of strings"},
		"inputs numbers":        {`{"inputs":[1,2]}`, "must be an array of strings"},
		"inputs nested object":  {`{"inputs":[{"a":1}]}`, "must be an array of strings"},
		"outputs not an array":  {`{"outputs":1}`, "must be an array of strings"},
	}

	for label, tc := range cases {
		tc := tc
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			_, err := ParseImport(tc.id)
			if err == nil {
				t.Fatalf("ParseImport(%q) returned nil error, want one", tc.id)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("ParseImport(%q) error = %q, want it to contain %q", tc.id, err.Error(), tc.substr)
			}
		})
	}
}

// TestParseImportErrorDoesNotEchoPayload guards the diagnostic surface: the
// error must name the offending key, never repeat the whole import ID.
func TestParseImportErrorDoesNotEchoPayload(t *testing.T) {
	t.Parallel()
	_, err := ParseImport(`{"inputs":["secret-a","secret-b"],"outupts":["secret-c"]}`)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"secret-a", "secret-b", "secret-c"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q echoes %q from the payload", err.Error(), secret)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/accumulate/ -run TestParseImport -v
```

Expected: FAIL with "undefined: ParseImport".

- [ ] **Step 3: Write `internal/accumulate/import.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ImportSeed is the parsed form of an import ID. A missing key is an empty
// slice, never nil, so the caller always writes a concrete list to state.
type ImportSeed struct {
	Inputs  []string
	Outputs []string
}

// ParseImport parses an import ID, which is a JSON object whose values are
// arrays of strings:
//
//	{"inputs":["a","b"],"outputs":["a","b","c"]}
//
// A resource with no external system to discover it from cannot be imported by
// a plain passthrough ID, so the ID carries the seed itself.
//
// Errors name the expected shape and quote the offending key only, never the
// full payload: a diagnostic reaches the console and CI logs.
func ParseImport(id string) (ImportSeed, error) {
	seed := ImportSeed{Inputs: []string{}, Outputs: []string{}}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(id), &object); err != nil || object == nil {
		return ImportSeed{}, fmt.Errorf(
			"import ID must be a JSON object with optional \"inputs\" and \"outputs\" arrays of strings, " +
				"for example {\"inputs\":[\"a\"],\"outputs\":[\"a\",\"b\"]}")
	}

	// Sorted so a payload with more than one mistake reports the same key every
	// run rather than whichever the map happened to yield first.
	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if k != "inputs" && k != "outputs" {
			return ImportSeed{}, fmt.Errorf(
				"import ID key %q is not recognized; the only recognized keys are \"inputs\" and \"outputs\"", k)
		}
		values, err := stringArray(object[k])
		if err != nil {
			return ImportSeed{}, fmt.Errorf("import ID key %q %v", k, err)
		}
		if k == "inputs" {
			seed.Inputs = values
		} else {
			seed.Outputs = values
		}
	}
	return seed, nil
}

// stringArray decodes a JSON value that must be an array of strings. A JSON
// null decodes into a nil slice with no error, so it is rejected explicitly,
// as are numbers, objects, and booleans nested inside the array.
func stringArray(raw json.RawMessage) ([]string, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, fmt.Errorf("must be an array of strings")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			return nil, fmt.Errorf("must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/accumulate/ -v
```

Expected: PASS, including the boundary test.

- [ ] **Step 5: Commit**

```bash
git add internal/accumulate/import.go internal/accumulate/import_test.go
git commit -m "feat: JSON import seed parsing"
```

---

### Task 6: Provider value conversion

The single place `internal/provider` crosses between framework values and the pure functions. Keeping it in one file is what lets the resources read as direct transcriptions of the spec's algorithms.

**Files:**
- Create: `internal/provider/model.go`
- Test: `internal/provider/model_test.go`

**Interfaces:**
- Consumes: `github.com/hashicorp/terraform-plugin-framework/types`.
- Produces:
  - `func stringsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics)`
  - `func stringsFromSet(ctx context.Context, set types.Set) ([]string, diag.Diagnostics)`
  - `func listFromStrings(ctx context.Context, values []string) (types.List, diag.Diagnostics)`
  - `func setFromStrings(ctx context.Context, values []string) (types.Set, diag.Diagnostics)`

- [ ] **Step 1: Write the failing unit tests**

These are unit tests in package `provider`, not `provider_test`, because they exercise unexported functions.

`internal/provider/model_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestListFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":    nil,
		"empty":  {},
		"values": {"a", "b"},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := listFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("listFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("listFromStrings produced a null list; state must always hold a concrete value")
			}
			if got.IsUnknown() {
				t.Fatal("listFromStrings produced an unknown list")
			}
		})
	}
}

func TestSetFromStringsIsNeverNull(t *testing.T) {
	t.Parallel()

	for label, values := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		values := values
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			got, diags := setFromStrings(context.Background(), values)
			if diags.HasError() {
				t.Fatalf("setFromStrings: %v", diags.Errors())
			}
			if got.IsNull() {
				t.Fatal("setFromStrings produced a null set; state must always hold a concrete value")
			}
		})
	}
}

// TestSetFromStringsRequiresUniqueValues pins the framework behavior the
// resources depend on: a duplicate is a diagnostic, not a silent collapse.
// Every caller passes accumulate.Merge's output, which is unique by
// construction, so the diagnostic is unreachable in practice and is a bug
// signal if it ever appears.
func TestSetFromStringsRequiresUniqueValues(t *testing.T) {
	t.Parallel()
	got, diags := setFromStrings(context.Background(), []string{"a", "b", "a"})
	if !diags.HasError() {
		t.Fatalf("setFromStrings accepted duplicates and produced %v", got)
	}
}

func TestStringsFromListRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	list, diags := listFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("listFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromList(ctx, list)
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("round trip produced %v, want [a b]", got)
	}
}

func TestStringsFromListNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromList(context.Background(), types.ListNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromList: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromList(null) = %v, want nil", got)
	}
}

func TestStringsFromSetRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	set, diags := setFromStrings(ctx, []string{"a", "b"})
	if diags.HasError() {
		t.Fatalf("setFromStrings: %v", diags.Errors())
	}
	got, diags := stringsFromSet(ctx, set)
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if len(got) != 2 {
		t.Fatalf("round trip produced %v, want two elements", got)
	}
}

func TestStringsFromSetNullIsEmpty(t *testing.T) {
	t.Parallel()
	got, diags := stringsFromSet(context.Background(), types.SetNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("stringsFromSet: %v", diags.Errors())
	}
	if got != nil {
		t.Fatalf("stringsFromSet(null) = %v, want nil", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/provider/ -run 'TestListFromStrings|TestSetFromStrings|TestStringsFrom' -v
```

Expected: FAIL with "undefined: listFromStrings" and friends.

- [ ] **Step 3: Write `internal/provider/model.go`**

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringsFromList converts a list(string) to a Go slice. Null and unknown are
// both treated as absent and produce a nil slice with no diagnostic: a null
// required attribute is a schema violation the framework already reports, and
// an unknown value is resolved before Create or Update runs.
func stringsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(list.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// stringsFromSet is stringsFromList for a set(string). Element order is not
// meaningful for a set; the result is only ever fed to accumulate.Merge, which
// is a union.
func stringsFromSet(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// listFromStrings converts a Go slice to a concrete list(string). A nil slice
// becomes an empty list, never null: an empty accumulated history must be
// written as [] so state round-trips without a diff against an empty config.
func listFromStrings(ctx context.Context, values []string) (types.List, diag.Diagnostics) {
	if values == nil {
		values = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, values)
}

// setFromStrings is listFromStrings for a set(string). values must already be
// unique: the framework reports a duplicate as a diagnostic rather than
// collapsing it silently. Every caller passes accumulate.Merge's output, which
// is unique, so a diagnostic here means a caller bug rather than bad input.
func setFromStrings(ctx context.Context, values []string) (types.Set, diag.Diagnostics) {
	if values == nil {
		values = []string{}
	}
	return types.SetValueFrom(ctx, types.StringType, values)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/provider/ -v
```

Expected: PASS for the new conversion tests plus `TestProviderSchema`, `TestUserFacingStringsUseEmDashes`, and `TestEveryGoFileHasTheSPDXHeader`.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/model.go internal/provider/model_test.go
git commit -m "feat: shared list and set value conversion"
```

---

### Task 7: `accumulator_list` resource

Schema, Create, Update, Read, Delete for the list resource, plus its acceptance tests and the shared test helpers the set resource's tests reuse. Import is Task 9.

**Files:**
- Create: `internal/provider/resource_list.go`
- Modify: `internal/provider/provider.go` (register `NewListResource`)
- Modify: `internal/provider/provider_test.go` (add `stringList`, `expectEmptyAfterRefresh`)
- Test: `internal/provider/resource_list_test.go`

**Interfaces:**
- Consumes: `accumulate.Trim`, `accumulate.Append`, `accumulate.HashID`; `stringsFromList`, `listFromStrings`.
- Produces:
  - `func NewListResource() resource.Resource`
  - `type listResourceModel struct { Inputs types.List; Length types.Int64; TriggersReset types.String; TriggersReplacement types.String; Outputs types.List; ID types.String }`
  - test-only `func stringList(values ...string) []knownvalue.Check`
  - test-only `func expectEmptyAfterRefresh() resource.ConfigPlanChecks`

- [ ] **Step 1: Write the failing acceptance tests**

`internal/provider/resource_list_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// listConfig renders an accumulator_list. extra is a raw attribute line or two,
// used for the trigger attributes; pass "" when there are none.
func listConfig(inputs string, length int, extra string) string {
	return fmt.Sprintf(`
resource "accumulator_list" "test" {
  inputs = %s
  length = %d
%s
}
`, inputs, length, extra)
}

func expectListOutputs(outputs ...string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("accumulator_list.test",
		tfjsonpath.New("outputs"), knownvalue.ListExact(stringList(outputs...)))
}

func TestAccListCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: listConfig(`["a"]`, 1, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectListOutputs("a"),
				// id is HashID of the inputs at creation; sha256(`["a"]`).
				statecheck.ExpectKnownValue("accumulator_list.test", tfjsonpath.New("id"),
					knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccListTrimsOnCreate covers "the last item(s) always win" when the initial
// list is longer than length.
func TestAccListTrimsOnCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:           listConfig(`["a", "b"]`, 1, ""),
			ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b")},
			ConfigPlanChecks:  expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccListAppendAndTrim is spec section 6.1's worked example: create ["a"]
// length 2, then ["b"] giving ["a","b"], then ["c"] giving ["b","c"].
func TestAccListAppendAndTrim(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["b"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["c"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListEmptyInputsRetainsHistory is spec section 4 invariant 2: inputs =
// [] never erases history.
func TestAccListEmptyInputsRetainsHistory(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`[]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListLengthShrinkAndGrow covers a length change with unchanged inputs:
// shrinking discards the oldest entries, and growing does not resurrect them.
func TestAccListLengthShrinkAndGrow(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a", "b", "c"]`, 3, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["a", "b", "c"]`, 2, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["a", "b", "c"]`, 3, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b", "c")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListLengthZero empties outputs and keeps them empty.
func TestAccListLengthZero(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 1, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`[]`, 0, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs(t)},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListUnchangedInputs is spec section 4 invariant 1: a second plan after
// a successful apply is empty.
func TestAccListUnchangedInputs(t *testing.T) {
	config := listConfig(`["a", "b"]`, 2, "")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            config,
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:           config,
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListReset covers triggers_reset discarding history even when inputs did
// not change.
func TestAccListReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            listConfig(`["b"]`, 2, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// triggers_reset changes while inputs stay ["b"]: history is
				// discarded and outputs reseeds from inputs.
				Config:            listConfig(`["b"]`, 2, `  triggers_reset = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccListReplacement covers triggers_replacement: a change forces a replace
// that reseeds outputs from inputs, discarding history.
func TestAccListReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            listConfig(`["a"]`, 2, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// If this were an in-place update the output would be ["a","c"];
				// a replacement reseeds from ["c"].
				Config:            listConfig(`["c"]`, 2, `  triggers_replacement = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectListOutputs("c")},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_list.test", plancheck.ResourceActionReplace),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}
```

- [ ] **Step 2: Add the shared test helpers**

Append to `internal/provider/provider_test.go`. Add `"github.com/hashicorp/terraform-plugin-testing/knownvalue"` and `"github.com/hashicorp/terraform-plugin-testing/plancheck"` to its imports.

```go
// stringList widens string values into the knownvalue check slice
// ListExact/SetExact take.
func stringList(values ...string) []knownvalue.Check {
	checks := make([]knownvalue.Check, 0, len(values))
	for _, v := range values {
		checks = append(checks, knownvalue.StringExact(v))
	}
	return checks
}

// expectEmptyAfterRefresh is the check spec section 4 invariant 1 requires of
// every step that applies configuration: after the apply and a refresh, the
// plan proposes nothing.
func expectEmptyAfterRefresh() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
make testacc TESTARGS='-run TestAccList'
```

Expected: FAIL. OpenTofu reports the provider does not define a resource type named `accumulator_list`.

- [ ] **Step 4: Write `internal/provider/resource_list.go`**

Import is deliberately absent here; Task 9 adds `ImportState`.

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var _ resource.Resource = (*listResource)(nil)

// listResource accumulates inputs across applies, keeping the most recent
// length entries. State is the only store, so Read and Delete are no-ops.
type listResource struct{}

// NewListResource returns the accumulator_list resource.
func NewListResource() resource.Resource {
	return &listResource{}
}

// listResourceModel is accumulator_list's state model.
type listResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	Length              types.Int64  `tfsdk:"length"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	Outputs             types.List   `tfsdk:"outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *listResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_list"
}

func (r *listResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies, keeping the most recent `length` " +
			"entries in `outputs`. Each apply whose `inputs` differs from the previous apply appends " +
			"the whole new list; changing only `length` re-trims the existing history without " +
			"appending. A change to `inputs` is treated as new input even when it overlaps what came " +
			"before, and re-submitting a value appends it again: there is no deduplication here.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The values to accumulate. Terraform stores `inputs` in state so " +
					"the provider can compare the planned list against the previous apply's list.",
			},
			"length": schema.Int64Attribute{
				Required:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
				MarkdownDescription: "How many of the most recent values to keep in `outputs`. " +
					"`length = 0` produces an empty `outputs`. Must be zero or greater.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated history and reseed " +
					"`outputs` from `inputs`. Any change counts, including from null to a value. Leave " +
					"it null for no reset.",
			},
			"triggers_replacement": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				MarkdownDescription: "Change this value to force a replacement (destroy and recreate), " +
					"which reseeds `outputs` from `inputs`. Any change counts, including from null to a " +
					"value. Leave it null for no replacement.",
			},
			"outputs": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The accumulated history, oldest first, trimmed to at most " +
					"`length` elements. Computed; never configured. Deliberately has no " +
					"`UseStateForUnknown`: it must plan as unknown whenever an input changes, because " +
					"the applied value depends on the prior state.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the `inputs` present when the resource was created. Stable across " +
					"in-place updates; recomputed on replacement.",
			},
		},
	}
}

// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
func (r *listResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	outputs, d := listFromStrings(ctx, accumulate.Trim(inputs, int(plan.Length.ValueInt64())))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a structural no-op. The framework copies the prior state into the
// Read response before calling Read, so returning it unchanged means a refresh
// can never perturb an attribute. That is what makes a second plan after a
// successful apply empty.
func (r *listResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

// Update applies the accumulation algorithm from spec section 6.1.
//
// triggers_replacement is never inspected here: a change to it plans a
// replacement, and Update is not called.
func (r *listResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateInputs, d := stringsFromList(ctx, state.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromList(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	length := int(plan.Length.ValueInt64())

	var outputs []string
	switch {
	case !plan.TriggersReset.Equal(state.TriggersReset):
		// Reset: discard history and reseed from the planned inputs.
		outputs = accumulate.Trim(planInputs, length)
	case !plan.Inputs.Equal(state.Inputs):
		// Inputs changed: append the whole new list to the prior history.
		outputs = accumulate.Append(stateOutputs, planInputs, length)
	default:
		// Only length changed: re-trim the existing history.
		outputs = accumulate.Trim(stateOutputs, length)
	}

	out, d := listFromStrings(ctx, outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = out
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there is
// nothing external to tear down.
func (r *listResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
```

- [ ] **Step 5: Register the resource**

In `internal/provider/provider.go`:

```go
func (p *accumulatorProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewListResource,
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
go test ./internal/provider/ -count=1
make testacc TESTARGS='-run TestAccList'
```

Expected: unit tests PASS, and every `TestAccList*` PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/resource_list.go internal/provider/provider.go internal/provider/provider_test.go internal/provider/resource_list_test.go
git commit -m "feat: accumulator_list resource"
```

---

### Task 8: `accumulator_set` resource

Schema, Create, Update, Read, Delete for the set resource, plus its acceptance tests. The schema and algorithms mirror `accumulator_list` except that there is no `length` and Update always unions.

**Files:**
- Create: `internal/provider/resource_set.go`
- Modify: `internal/provider/provider.go` (register `NewSetResource`)
- Modify: `internal/provider/provider_test.go` (extend `TestProviderSchema` with one of each resource)
- Test: `internal/provider/resource_set_test.go`

**Interfaces:**
- Consumes: `accumulate.Merge`, `accumulate.HashID`; `stringsFromList`, `stringsFromSet`, `setFromStrings`.
- Produces:
  - `func NewSetResource() resource.Resource`
  - `type setResourceModel struct { Inputs types.List; TriggersReset types.String; TriggersReplacement types.String; Outputs types.Set; ID types.String }`

- [ ] **Step 1: Write the failing acceptance tests**

`internal/provider/resource_set_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// setConfig renders an accumulator_set. extra is a raw attribute line or two,
// used for the trigger attributes; pass "" when there are none.
func setConfig(inputs string, extra string) string {
	return fmt.Sprintf(`
resource "accumulator_set" "test" {
  inputs = %s
%s
}
`, inputs, extra)
}

func expectSetOutputs(outputs ...string) statecheck.StateCheck {
	return statecheck.ExpectKnownValue("accumulator_set.test",
		tfjsonpath.New("outputs"), knownvalue.SetExact(stringList(outputs...)))
}

// TestAccSetUnion is spec section 4 invariant 4: successive updates accumulate,
// each value appearing once, and nothing is forgotten.
func TestAccSetUnion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            setConfig(`["b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// Re-submitting values that are already in the set changes
				// nothing, and the set never contains a duplicate.
				Config:            setConfig(`["a", "b"]`, ""),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetDeduplicatesWithinOneApply covers duplicates inside a single
// inputs list.
func TestAccSetDeduplicatesWithinOneApply(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:            setConfig(`["a", "a", "b"]`, ""),
			ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a", "b")},
			ConfigPlanChecks:  expectEmptyAfterRefresh(),
		}},
	})
}

// TestAccSetReset covers triggers_reset discarding history and reseeding from
// inputs, which here forgets "a" entirely.
func TestAccSetReset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  triggers_reset = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				Config:            setConfig(`["b"]`, `  triggers_reset = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("b")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetReplacement covers triggers_replacement reseeding from inputs
// instead of unioning.
func TestAccSetReplacement(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:            setConfig(`["a"]`, `  triggers_replacement = "v1"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("a")},
				ConfigPlanChecks:  expectEmptyAfterRefresh(),
			},
			{
				// An in-place union would give {a, c}; a replacement gives {c}.
				Config:            setConfig(`["c"]`, `  triggers_replacement = "v2"`),
				ConfigStateChecks: []statecheck.StateCheck{expectSetOutputs("c")},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("accumulator_set.test", plancheck.ResourceActionReplace),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

// TestAccSetCreateIdentifiesFromInputs pins the creation identifier.
func TestAccSetCreateIdentifiesFromInputs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: setConfig(`["a"]`, ""),
			ConfigStateChecks: []statecheck.StateCheck{
				expectSetOutputs("a"),
				// sha256(`["a"]`)
				statecheck.ExpectKnownValue("accumulator_set.test", tfjsonpath.New("id"),
					knownvalue.StringExact("0eb5b8d6f81bc677da8a08567cc4fa9a06a57e9ec8da85ed73a7f62727996002")),
			},
			ConfigPlanChecks: expectEmptyAfterRefresh(),
		}},
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
make testacc TESTARGS='-run TestAccSet'
```

Expected: FAIL. OpenTofu reports the provider does not define a resource type named `accumulator_set`.

- [ ] **Step 3: Write `internal/provider/resource_set.go`**

Import is deliberately absent here; Task 9 adds `ImportState`.

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nijave/terraform-provider-accumulator/internal/accumulate"
)

var _ resource.Resource = (*setResource)(nil)

// setResource accumulates inputs across applies into a deduplicated set. State
// is the only store, so Read and Delete are no-ops.
type setResource struct{}

// NewSetResource returns the accumulator_set resource.
func NewSetResource() resource.Resource {
	return &setResource{}
}

// setResourceModel is accumulator_set's state model.
type setResourceModel struct {
	Inputs              types.List   `tfsdk:"inputs"`
	TriggersReset       types.String `tfsdk:"triggers_reset"`
	TriggersReplacement types.String `tfsdk:"triggers_replacement"`
	Outputs             types.Set    `tfsdk:"outputs"`
	ID                  types.String `tfsdk:"id"`
}

func (r *setResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_set"
}

func (r *setResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Accumulates `inputs` across applies into a deduplicated `outputs` set. " +
			"Every value ever seen is retained once, in the order it was first observed, and a value " +
			"is only forgotten by a `triggers_reset` change or a replacement. `inputs` is a list, " +
			"matching the shape users write in configuration; order does not affect the result because " +
			"the values are unioned.",
		Attributes: map[string]schema.Attribute{
			"inputs": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The values to accumulate. Terraform stores `inputs` in state so " +
					"the provider can compare the planned list against the previous apply's list.",
			},
			"triggers_reset": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Change this value to discard the accumulated set and reseed " +
					"`outputs` from `inputs`, forgetting every value seen before the change. Any change " +
					"counts, including from null to a value. Leave it null for no reset.",
			},
			"triggers_replacement": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				MarkdownDescription: "Change this value to force a replacement (destroy and recreate), " +
					"which reseeds `outputs` from `inputs`. Any change counts, including from null to a " +
					"value. Leave it null for no replacement.",
			},
			"outputs": schema.SetAttribute{
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Every value ever accumulated, each appearing once. Computed; " +
					"never configured. Deliberately has no `UseStateForUnknown`: it must plan as unknown " +
					"whenever an input changes, because the applied value depends on the prior state.",
			},
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				MarkdownDescription: "A stable identifier: the lowercase hex SHA-256 of the canonical " +
					"JSON encoding of the `inputs` present when the resource was created. Stable across " +
					"in-place updates; recomputed on replacement.",
			},
		},
	}
}

// Create seeds outputs from the planned inputs. It also runs after a
// replacement, which is what makes triggers_replacement reseed.
func (r *setResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	outputs, d := setFromStrings(ctx, accumulate.Merge(nil, inputs))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = types.StringValue(accumulate.HashID(inputs))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a structural no-op, for the same reason as accumulator_list's: the
// framework copies prior state into the response, so a refresh cannot perturb
// an attribute.
func (r *setResource) Read(_ context.Context, _ resource.ReadRequest, _ *resource.ReadResponse) {
}

// Update applies the accumulation algorithm from spec section 6.2. No inputs
// comparison is needed: union is idempotent, so applying it whenever Update
// runs is correct, and Update does not run when nothing changed.
func (r *setResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state setResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planInputs, d := stringsFromList(ctx, plan.Inputs)
	resp.Diagnostics.Append(d...)
	stateOutputs, d := stringsFromSet(ctx, state.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	var merged []string
	if !plan.TriggersReset.Equal(state.TriggersReset) {
		// Reset: discard history and reseed from the planned inputs.
		merged = accumulate.Merge(nil, planInputs)
	} else {
		merged = accumulate.Merge(stateOutputs, planInputs)
	}

	outputs, d := setFromStrings(ctx, merged)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.Outputs = outputs
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op. The framework removes the resource from state and there is
// nothing external to tear down.
func (r *setResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
```

- [ ] **Step 4: Register the resource**

In `internal/provider/provider.go`:

```go
func (p *accumulatorProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewListResource,
		NewSetResource,
	}
}
```

- [ ] **Step 5: Extend `TestProviderSchema` to plan one of each resource**

Both resource types now exist, so the schema guard can exercise them. Replace the `Config` in `TestProviderSchema`:

```go
			Config: `
provider "accumulator" {}

resource "accumulator_list" "check" {
  inputs = []
  length = 0
}

resource "accumulator_set" "check" {
  inputs = []
}
`,
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
go test ./internal/provider/ -count=1
make testacc TESTARGS='-run TestAccSet'
```

Expected: unit tests PASS, and every `TestAccSet*` PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/resource_set.go internal/provider/provider.go internal/provider/provider_test.go internal/provider/resource_set_test.go
git commit -m "feat: accumulator_set resource"
```

---

### Task 9: Import for both resources

`ImportState` on each resource, using the shared `accumulate.ParseImport`, plus the import acceptance tests. State is the only store, so import takes an explicit JSON seed; a plain passthrough ID was rejected because it would import an empty resource with no way to put history in.

**Files:**
- Modify: `internal/provider/resource_list.go` (add `ImportState`)
- Modify: `internal/provider/resource_set.go` (add `ImportState`)
- Test: `internal/provider/import_test.go`

**Interfaces:**
- Consumes: `accumulate.ParseImport`, `accumulate.HashID`, `accumulate.Merge`; `stringsFromList`, `listFromStrings`, `setFromStrings`.
- Produces: nothing new; both resources now satisfy `resource.ResourceWithImportState`.

- [ ] **Step 1: Write the failing acceptance tests**

`internal/provider/import_test.go`. Uses `ImportStateCheck` rather than `ConfigStateChecks`, because state checks run during config (apply) steps, not import steps. `ImportStateVerify` is false because imported state deliberately carries a null `length` for the list resource and an empty `inputs` for the set resource, so it does not match the config.

```go
// SPDX-License-Identifier: GPL-3.0-or-later

package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// importConfig supplies the required configuration for the resource being
// imported. The imported history comes from the ID, not from this config.
func importConfig(resourceType string) string {
	if resourceType == "accumulator_list" {
		return `
resource "accumulator_list" "test" {
  inputs = ["a", "b"]
  length = 3
}
`
	}
	return `
resource "accumulator_set" "test" {
  inputs = ["d"]
}
`
}

// TestAccListImport seeds inputs and outputs from the JSON ID. A following
// apply must be empty: the configuration's inputs match the seeded inputs, so
// the settling apply only fills in length and re-trims the seeded outputs.
func TestAccListImport(t *testing.T) {
	config := importConfig("accumulator_list")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_list.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":["a","b"],"outputs":["a","b","c"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					// sha256(`["a","b"]`)
					if got := attrs["id"]; got != "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13" {
						return fmt.Errorf("id = %q, want the hash of the seeded inputs", got)
					}
					if got := attrs["inputs.#"]; got != "2" {
						return fmt.Errorf("inputs.# = %q, want 2", got)
					}
					if got := attrs["outputs.#"]; got != "3" {
						return fmt.Errorf("outputs.# = %q, want 3", got)
					}
					if got := attrs["outputs.2"]; got != "c" {
						return fmt.Errorf("outputs.2 = %q, want \"c\"", got)
					}
					return nil
				},
			},
			{
				// The settling apply. length goes null -> 3 and outputs is
				// re-trimmed from the seeded value; after that the plan is
				// empty.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectListOutputs("a", "b", "c"),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetImport seeds outputs from the JSON ID. inputs is seeded empty, and
// the settling apply unions the configured inputs into the seeded set.
func TestAccSetImport(t *testing.T) {
	config := importConfig("accumulator_set")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_set.test",
				ImportState:        true,
				ImportStateId:      `{"outputs":["a","b"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d resources, want 1", len(states))
					}
					attrs := states[0].Attributes
					// sha256(`["a","b"]`)
					if got := attrs["id"]; got != "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13" {
						return fmt.Errorf("id = %q, want the hash of the seeded outputs", got)
					}
					if got := attrs["outputs.#"]; got != "2" {
						return fmt.Errorf("outputs.# = %q, want 2", got)
					}
					// inputs is a required attribute; import seeds it as an
					// empty list rather than null.
					if got := attrs["inputs.#"]; got != "0" {
						return fmt.Errorf("inputs.# = %q, want 0", got)
					}
					return nil
				},
			},
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "b", "d"),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccSetImportIgnoresInputsKey pins that an inputs key in a set import ID is
// accepted and ignored: the set resource seeds no meaningful inputs.
func TestAccSetImportIgnoresInputsKey(t *testing.T) {
	config := importConfig("accumulator_set")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "accumulator_set.test",
				ImportState:        true,
				ImportStateId:      `{"inputs":["ignored"],"outputs":["a"]}`,
				ImportStatePersist: true,
				ImportStateVerify:  false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					attrs := states[0].Attributes
					if got := attrs["inputs.#"]; got != "0" {
						return fmt.Errorf("inputs.# = %q, want 0; the inputs key must be ignored", got)
					}
					return nil
				},
			},
			{
				// "ignored" must not appear in the set: only the seeded outputs
				// and the configured inputs are unioned.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					expectSetOutputs("a", "d"),
				},
				ConfigPlanChecks: expectEmptyAfterRefresh(),
			},
		},
	})
}

// TestAccImportMalformed runs the three malformed shapes against both
// resources. Each ExpectError pattern is a provider-only phrase that does not
// appear in the configuration text, so the test cannot pass on OpenTofu's echo
// of the config.
func TestAccImportMalformed(t *testing.T) {
	for _, resourceType := range []string{"accumulator_list", "accumulator_set"} {
		resourceType := resourceType
		for label, tc := range map[string]struct {
			id     string
			expect *regexp.Regexp
		}{
			"bad json":         {`not-json`, regexp.MustCompile(`must be a JSON object`)},
			"unknown key":      {`{"outupts":["a"]}`, regexp.MustCompile(`not recognized`)},
			"wrong value type": {`{"outputs":"a"}`, regexp.MustCompile(`must be an array of strings`)},
		} {
			tc := tc
			t.Run(resourceType+"/"+label, func(t *testing.T) {
				resource.Test(t, resource.TestCase{
					PreCheck:                 func() { testAccPreCheck(t) },
					ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
					Steps: []resource.TestStep{{
						Config:            importConfig(resourceType),
						ResourceName:      resourceType + ".test",
						ImportState:       true,
						ImportStateId:     tc.id,
						ImportStateVerify: false,
						ExpectError:       tc.expect,
					}},
				})
			})
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
make testacc TESTARGS='-run TestAccListImport|TestAccSetImport|TestAccImportMalformed'
```

Expected: FAIL. The import is rejected because the resources do not implement `ResourceWithImportState`, so OpenTofu reports an unsupported import.

- [ ] **Step 3: Add `ImportState` to the list resource**

In `internal/provider/resource_list.go`, extend the interface assertion. No new import is needed: `ImportState` uses `accumulate`, `types`, `context`, and `resource`, all already imported.

```go
var (
	_ resource.Resource                = (*listResource)(nil)
	_ resource.ResourceWithImportState = (*listResource)(nil)
)
```

Then append:

```go
// ImportState seeds the resource from a JSON object. length, triggers_reset,
// and triggers_replacement are left null: length is required configuration, so
// the next plan fills it in and Update re-trims the seeded outputs. Seeding
// inputs is what lets a matching configuration produce an empty plan instead of
// appending the configured list a second time.
func (r *listResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		// The error names the expected shape and quotes the offending key only.
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, seed.Inputs)
	resp.Diagnostics.Append(d...)
	outputs, d := listFromStrings(ctx, seed.Outputs)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &listResourceModel{
		Inputs:              inputs,
		Length:              types.Int64Null(),
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		Outputs:             outputs,
		ID:                  types.StringValue(accumulate.HashID(seed.Inputs)),
	})...)
}
```

- [ ] **Step 4: Add `ImportState` to the set resource**

In `internal/provider/resource_set.go`, extend the interface assertion:

```go
var (
	_ resource.Resource                = (*setResource)(nil)
	_ resource.ResourceWithImportState = (*setResource)(nil)
)
```

Then append:

```go
// ImportState seeds the set from a JSON object. outputs is deduplicated and
// id hashes the seeded outputs. inputs is seeded as an empty list rather than
// null: it is a required attribute, and an empty list is a valid non-null
// value, so nothing depends on tolerating a null required attribute in
// imported state. The next plan sets inputs from configuration and Update
// unions it into the seeded outputs. An inputs key in the ID is accepted and
// ignored.
func (r *setResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	seed, err := accumulate.ParseImport(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	inputs, d := listFromStrings(ctx, []string{})
	resp.Diagnostics.Append(d...)
	outputs, d := setFromStrings(ctx, accumulate.Merge(nil, seed.Outputs))
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &setResourceModel{
		Inputs:              inputs,
		TriggersReset:       types.StringNull(),
		TriggersReplacement: types.StringNull(),
		Outputs:             outputs,
		ID:                  types.StringValue(accumulate.HashID(seed.Outputs)),
	})...)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/... -count=1
make testacc TESTARGS='-run TestAccListImport|TestAccSetImport|TestAccImportMalformed'
```

Expected: all PASS.

- [ ] **Step 6: Run the whole acceptance suite**

```bash
make testacc
```

Expected: PASS for every `TestAcc*`. This is the point at which all of spec section 10.2's cases exist.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/resource_list.go internal/provider/resource_set.go internal/provider/import_test.go
git commit -m "feat: JSON-seeded import for both resources"
```

---

### Task 10: Generated documentation and examples

`tfplugindocs` renders `docs/` from the schema plus `examples/`. Schema export needs `tofu` because `tfplugindocs` otherwise downloads Terraform, which this project forbids.

**Files:**
- Create: `templates/index.md.tmpl`
- Create: `examples/provider/provider.tf`
- Create: `examples/resources/accumulator_list/resource.tf`, `examples/resources/accumulator_list/import.sh`
- Create: `examples/resources/accumulator_set/resource.tf`, `examples/resources/accumulator_set/import.sh`
- Create: `tools/gen-schema.sh`
- Modify: `GNUmakefile` (add the `docs` target)
- Generated and committed: `docs/index.md`, `docs/resources/accumulator_list.md`, `docs/resources/accumulator_set.md`

**Interfaces:**
- Consumes: the provider schema and examples.
- Produces: `make docs` leaving the tree clean.

- [ ] **Step 1: Write the doc template**

`templates/index.md.tmpl`. `tfplugindocs` substitutes `{{ .SchemaMarkdown }}`.

```markdown
# terraform-provider-accumulator

Resources that accumulate a value over successive applies while keeping a fixed
history length. The accumulated history lives in Terraform/OpenTofu state, so
there is no endpoint, no credential, and no external store to stand up: the
provider is two resources and a small amount of pure list and set logic.

## Example

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
```

Each apply that changes `inputs` appends the new value; the list keeps only the
most recent `length` entries.

## Requirements

- OpenTofu >= 1.10 (the primary target, and what CI tests) or Terraform >= 1.10.
  Terraform is expected to work but is not tested.
- Go >= 1.25 to build.

## History lives in state

`terraform state rm`, moving a resource between workspaces or state files, and
state loss all discard the accumulated history. `inputs` is stored in state, so
`tofu plan` and `tofu state show` expose it: do not accumulate secrets.

## License

GPL-3.0-or-later. Every dependency is GPL-compatible (MPL-2.0, BSD-3-Clause,
MIT, or Apache-2.0).

---

{{ .SchemaMarkdown }}
```

- [ ] **Step 2: Write the examples**

`examples/provider/provider.tf`:

```hcl
terraform {
  required_providers {
    accumulator = {
      source  = "nijave/accumulator"
      version = "~> 0.1"
    }
  }
}

# The provider takes no configuration. State is the only store, so there is no
# endpoint, no credential, and no client.
provider "accumulator" {}
```

`examples/resources/accumulator_list/resource.tf`:

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
```

`examples/resources/accumulator_list/import.sh`:

```sh
# The import ID is a JSON object. inputs and outputs are both optional and
# default to empty. Seeding inputs lets a matching configuration plan cleanly
# instead of appending the configured list a second time.
terraform import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
```

`examples/resources/accumulator_set/resource.tf`:

```hcl
resource "accumulator_set" "seen_hosts" {
  inputs = [var.hostname]
}

output "seen_hosts" {
  value = accumulator_set.seen_hosts.outputs
}
```

`examples/resources/accumulator_set/import.sh`:

```sh
# accumulator_set seeds outputs (deduplicated) and hashes them for id. An
# inputs key, if present, is accepted and ignored.
terraform import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
```

- [ ] **Step 3: Write `tools/gen-schema.sh`**

Adapted from the sibling pki repository's `tools/gen-schema.sh`; read that file first. `tfplugindocs` resolves the provider schema by shelling out to a binary literally named `terraform` and assumes providers publish under `registry.terraform.io`. Neither holds for OpenTofu, so the schema is exported with `tofu` through a `dev_overrides` CLI config and the registry key is rewritten to the bare provider name `tfplugindocs --providers-schema` expects.

```bash
#!/usr/bin/env bash
# tfplugindocs (github.com/hashicorp/terraform-plugin-docs) resolves the
# provider schema by shelling out to a binary literally named "terraform" and
# assumes providers publish under registry.terraform.io. Neither holds for
# OpenTofu: it isn't named "terraform", so tfplugindocs falls back to
# downloading the latest real Terraform release, silently pulling a
# BUSL-licensed binary into doc generation, which is exactly what this
# project's `license` job exists to forbid, and a source of doc drift every time
# HashiCorp cuts a release. OpenTofu also defaults unqualified provider
# addresses to registry.opentofu.org, so it can't simply be renamed/wrapped as
# "terraform" either.
#
# Export the schema ourselves with `tofu`, using a dev_overrides CLI config
# (the standard local-provider-development mechanism) so no registry lookup or
# provider install is involved, then rewrite the registry.opentofu.org key to
# the bare provider name tfplugindocs's --providers-schema loader expects.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

command -v tofu >/dev/null || { echo "tofu not found in PATH; OpenTofu >= 1.11 is required" >&2; exit 1; }

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

(cd .. && go build -o "$workdir/terraform-provider-accumulator" .)

cat > "$workdir/dev.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "hashicorp/accumulator" = "$workdir"
  }
  direct {}
}
EOF

mkdir "$workdir/work"
cat > "$workdir/work/main.tf" <<'EOF'
terraform {
  required_providers {
    accumulator = {
      source = "hashicorp/accumulator"
    }
  }
}
provider "accumulator" {}
EOF

TF_CLI_CONFIG_FILE="$workdir/dev.tfrc" tofu -chdir="$workdir/work" providers schema -json \
  | sed 's#"registry\.opentofu\.org/hashicorp/accumulator"#"accumulator"#' \
  > schema.json
```

Then:

```bash
chmod +x tools/gen-schema.sh
```

- [ ] **Step 4: Add the `docs` target to the `GNUmakefile`**

```makefile
.PHONY: docs
docs:
	./tools/gen-schema.sh
	cd tools && go generate ./...
```

- [ ] **Step 5: Generate the documentation**

```bash
make docs
```

Then inspect the result:

```bash
ls docs docs/resources
```

Expected: `docs/index.md`, `docs/resources/accumulator_list.md`, `docs/resources/accumulator_set.md`.

- [ ] **Step 6: Verify nothing hand-written was clobbered and the schema export is ignored**

```bash
git status --porcelain docs/superpowers
git status --porcelain tools/schema.json
```

Expected: no output from either command. `docs/superpowers/` is hand-written and must be untouched; `tools/schema.json` is gitignored and must not appear as untracked.

- [ ] **Step 7: Commit**

```bash
git add templates/index.md.tmpl examples/ tools/gen-schema.sh GNUmakefile docs/index.md docs/resources/accumulator_list.md docs/resources/accumulator_set.md
git commit -m "docs: generated provider documentation and examples"
```

---

### Task 11: CI, release, and README

The build, CI, and release pipeline from spec section 12. `main.go`, `terraform-registry-manifest.json`, and `LICENSE` already exist.

**Files:**
- Create: `.github/workflows/test.yml`
- Create: `.github/workflows/release.yml`
- Create: `.github/dependabot.yml`
- Create: `.goreleaser.yml`
- Create: `README.md`

**Interfaces:**
- Consumes: `make docs`, `make testacc`, the Go modules.
- Produces: the CI gate and release assets.

- [ ] **Step 1: Write `.github/workflows/test.yml`**

```yaml
# Runs on pull requests and on pushes to main. Scoping push to main avoids a
# duplicate run when a branch in this repo also has an open PR.
name: Tests
on:
  pull_request:
    paths-ignore:
      - 'README.md'
      - 'SPEC.md'
      - 'docs/superpowers/**'
  push:
    branches: [main]
    paths-ignore:
      - 'README.md'
      - 'SPEC.md'
      - 'docs/superpowers/**'

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      - run: go mod download
      - run: go build -v ./...
      - name: gofmt
        run: |
          test -z "$(gofmt -l .)" || (gofmt -l . && echo "run 'make fmt'" && exit 1)
      - run: go vet ./...

  unit:
    name: Unit Tests
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      - run: go mod download
      - name: go test
        run: go test -v -cover ./internal/...

  generate:
    name: Generated Docs Are Current
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      # tools/gen-schema.sh shells out to `tofu` to export the provider schema
      # (see that script for why tfplugindocs can't be pointed at OpenTofu
      # directly). Pinned exactly because schema export output changes when the
      # exporting CLI crosses a feature threshold, which is doc drift. OpenTofu
      # rather than Terraform, so no BUSL-licensed binary is used in CI.
      - uses: opentofu/setup-opentofu@v2
        with:
          tofu_version: '1.12.4'
          tofu_wrapper: false
      - name: Generate
        run: make docs
      - name: Check for uncommitted changes
        run: |
          git diff --compact-summary --exit-code || \
            (echo; echo "Generated documentation is out of date. Run 'make docs' and commit the result."; exit 1)

  acceptance:
    name: Acceptance (OpenTofu ${{ matrix.tofu }})
    needs: build
    runs-on: ubuntu-latest
    timeout-minutes: 25
    strategy:
      fail-fast: false
      matrix:
        # The support floor is OpenTofu 1.10, the oldest release line still
        # receiving security support; 1.12 is the newest. Terraform is not
        # tested: it is BUSL-licensed and is not the reference platform.
        tofu:
          - '1.10.*'
          - '1.12.*'
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      - uses: opentofu/setup-opentofu@v2
        with:
          tofu_version: ${{ matrix.tofu }}
          tofu_wrapper: false
      - run: go mod download
      # Resolve tofu's absolute path: terraform-plugin-testing requires an
      # absolute TF_ACC_TERRAFORM_PATH on some versions, and the bare name on
      # PATH is not guaranteed to satisfy it. This mirrors the Makefile's
      # testacc target, which is known to work.
      - name: Resolve tofu path
        run: echo "TF_ACC_TERRAFORM_PATH=$(command -v tofu)" >> "$GITHUB_ENV"
      - name: Acceptance tests
        timeout-minutes: 20
        env:
          TF_ACC: '1'
          # terraform-plugin-testing performs no version check against a binary
          # already present at this path, so the harness drives OpenTofu
          # directly and never downloads Terraform.
          TF_ACC_TERRAFORM_PATH: ${{ env.TF_ACC_TERRAFORM_PATH }}
          # Required, not optional. terraform-plugin-testing defaults its
          # reattach host to registry.terraform.io while still registering the
          # legacy "-" namespace as a reattach candidate, and OpenTofu's address
          # parser rejects that pairing outright. Without this, EVERY acceptance
          # test fails before it reaches the provider.
          TF_ACC_PROVIDER_HOST: registry.opentofu.org
        run: go test -v -cover -timeout 20m ./internal/provider/

  license:
    name: Dependency Licenses
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      - name: Install go-licenses
        run: go install github.com/google/go-licenses@latest
      # The project is GPL-3.0-or-later, so every dependency must be
      # GPLv3-compatible. The forbidden set is what actually threatens that:
      # BUSL-1.1 (Terraform CLI since 1.6) and the copyleft-incompatible
      # licenses. Apache-2.0 is allowed because this is GPLv3, not GPLv2. If the
      # license is ever downgraded, re-audit spec section 13 first.
      - name: Check licenses
        run: |
          go-licenses check ./... \
            --disallowed_types=forbidden,restricted,unknown \
            --ignore=github.com/nijave/terraform-provider-accumulator
      - name: Report licenses
        if: always()
        run: go-licenses report ./... --ignore=github.com/nijave/terraform-provider-accumulator || true
```

- [ ] **Step 2: Write `.github/workflows/release.yml`**

```yaml
# Publishes release assets when a v* tag is pushed.
#
# Requires two repository secrets: GPG_PRIVATE_KEY and PASSPHRASE. The registry
# verifies the detached signature over the checksum file against the public key
# registered with it.
name: release
on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - name: Checkout
        uses: actions/checkout@v7
      - name: Unshallow
        run: git fetch --prune --unshallow
      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version-file: 'go.mod'
          cache: true
      - name: Import GPG key
        uses: crazy-max/ghaction-import-gpg@v7
        id: import_gpg
        with:
          gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
          passphrase: ${{ secrets.PASSPHRASE }}
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GPG_FINGERPRINT: ${{ steps.import_gpg.outputs.fingerprint }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 3: Write `.goreleaser.yml`**

```yaml
# https://goreleaser.com
version: 2
before:
  hooks:
    - go mod tidy
builds:
  - env:
      # goreleaser does not work with CGO, and a CGO-free binary also runs in
      # restricted CI environments. Nothing in the dependency set needs cgo.
      - CGO_ENABLED=0
    mod_timestamp: '{{ .CommitTimestamp }}'
    flags:
      - -trimpath
    ldflags:
      - '-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}'
    goos:
      - freebsd
      - windows
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    binary: '{{ .ProjectName }}_v{{ .Version }}'
archives:
  - formats: [zip]
    name_template: '{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}'
checksum:
  extra_files:
    - glob: 'terraform-registry-manifest.json'
      name_template: '{{ .ProjectName }}_{{ .Version }}_manifest.json'
  name_template: '{{ .ProjectName }}_{{ .Version }}_SHA256SUMS'
  algorithm: sha256
signs:
  - artifacts: checksum
    args:
      - "--batch"
      - "--local-user"
      - "{{ .Env.GPG_FINGERPRINT }}"
      - "--output"
      - "${signature}"
      - "--detach-sign"
      - "${artifact}"
release:
  extra_files:
    - glob: 'terraform-registry-manifest.json'
      name_template: '{{ .ProjectName }}_{{ .Version }}_manifest.json'
  # Draft, so the release can be examined before it is published to the
  # registry.
  draft: true
changelog:
  disable: true
```

- [ ] **Step 4: Write `.github/dependabot.yml`**

```yaml
# https://docs.github.com/en/code-security/dependabot/dependabot-version-updates
version: 2
updates:
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
    groups:
      actions:
        patterns: ["*"]

  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    groups:
      # The plugin-framework modules expect to move in lockstep, so grouping
      # them avoids a set of PRs that individually fail to build.
      terraform-plugin:
        patterns: ["github.com/hashicorp/terraform-plugin-*"]
      golang-x:
        patterns: ["golang.org/x/*"]

  # The tools module is separate, so it needs its own entry or tfplugindocs
  # never gets updated.
  - package-ecosystem: "gomod"
    directory: "/tools"
    schedule:
      interval: "weekly"
    groups:
      tools:
        patterns: ["*"]
```

- [ ] **Step 5: Write `README.md`**

```markdown
# terraform-provider-accumulator

Terraform/OpenTofu resources that accumulate a value over successive applies
while keeping a fixed history length, with the history stored in state. There is
no endpoint, no credential, no database, and no object store: the provider is
two resources and a small amount of pure list and set logic.

Terraform resources model a desired end state, not a history. When a value needs
to accumulate over successive applies and the history must be readable by
`tofu output` without standing up an external system, these resources are the
declarative equivalent of an LRU.

## Example

```hcl
resource "accumulator_list" "recent_deploys" {
  inputs = [var.deploy_sha]
  length = 5
}

output "recent_deploys" {
  value = accumulator_list.recent_deploys.outputs
}
```

## Resources

- **`accumulator_list`** keeps the most recent `length` inputs. Each apply whose
  `inputs` differs from the previous apply appends the whole new list; a
  `length` change re-trims the existing history. `inputs = []` never erases
  history.
- **`accumulator_set`** remembers every value it has ever seen, each once, in
  the order first observed.

Both resources carry `triggers_reset` (discard history and reseed from `inputs`)
and `triggers_replacement` (force a replacement that reseeds). Both are
importable with a JSON seed, since there is no external system to discover them
from:

```sh
tofu import accumulator_list.recent_deploys '{"inputs":["a","b"],"outputs":["a","b","c"]}'
tofu import accumulator_set.seen_hosts '{"outputs":["a","b"]}'
```

## History lives in state

`terraform state rm`, moving a resource between workspaces or state files, and
state loss all discard the accumulated history. `inputs` is stored in state, so
`tofu plan` and `tofu state show` expose it: do not accumulate secrets.

## Documentation

The [provider documentation](docs/index.md) lists both resources. The provider
has no configuration block.

## Requirements

- OpenTofu >= 1.10 (primary target, what CI tests) or Terraform >= 1.10
  (expected to work, not tested)
- Go >= 1.25 to build

## Development

```sh
make test      # unit tests
make testacc   # acceptance tests; requires tofu >= 1.10 on PATH
make docs      # regenerate docs/ ; requires tofu >= 1.11
```

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
```

- [ ] **Step 6: Verify the pipeline definitions parse and the docs job would be clean**

```bash
make docs
git status --porcelain
```

Expected: `make docs` regenerates the same files, so `git status --porcelain` shows only the files this task added (workflows, goreleaser, dependabot, README). Any change under `docs/` means Task 10's generated files are stale and must be committed with this task instead.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/test.yml .github/workflows/release.yml .github/dependabot.yml .goreleaser.yml README.md
git commit -m "ci: build, test, docs, acceptance, license, and release pipeline"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
| --- | --- |
| 3 Stack and repository layout | 1 (module, main, provider, tools, manifest), 2 through 9 (files), 10 (docs, examples, gen-schema) |
| 4 Concepts and invariants | 7 (invariants 1, 2, 3), 8 (invariant 4) |
| 5 Resource schemas | 7, 8 |
| 6 Accumulation algorithm | 2, 3, 7, 8 |
| 6.3 Pure functions | 2 (`Trim`, `Append`), 3 (`Merge`) |
| 6.4 Read and Delete | 7, 8 |
| 7 Resource identity | 4 (`HashID`), 7, 8, 9 |
| 8 Import | 5 (`ParseImport`), 9 |
| 9 Error handling | 5 (payload-free errors), 9 (diagnostics) |
| 10.1 Unit tests | 2, 3, 4, 5, 6, 1 (guard tests) |
| 10.2 Acceptance tests | 7, 8, 9 |
| 11 Documentation | 10 |
| 12 Build, CI, and release | 1 (`GNUmakefile`), 10 (`docs`), 11 (workflows, goreleaser, dependabot) |
| 13 License | 1 (LICENSE, SPDX guard), 11 (license job) |
| 14 Decisions: no `ModifyPlan` | Global Constraints; 7, 8 schemas |
| 15 Limitations | 10 (template), 11 (README) |

**Placeholder scan:** no "TBD", "implement later", or "similar to Task N", and every code step contains the code an engineer needs. Every `import` block in every listed file contains only packages that file uses, so the files compile as written.

**Type consistency:** `Trim`, `Append`, `Merge`, `HashID`, `ImportSeed`, `ParseImport`, `stringsFromList`, `stringsFromSet`, `listFromStrings`, `setFromStrings`, `NewListResource`, `NewSetResource`, `listResourceModel`, `setResourceModel`, `stringList`, `expectEmptyAfterRefresh`, `expectListOutputs`, `expectSetOutputs`, and `importConfig` are used with the same names and signatures everywhere they appear. Both `expectListOutputs` and `expectSetOutputs` take only the output values and return a `statecheck.StateCheck`; both are called that way in Tasks 7 through 9. `importConfig` is defined in Task 9 and used there only.
