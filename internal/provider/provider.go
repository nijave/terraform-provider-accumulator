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
	return []func() resource.Resource{
		NewListResource,
		NewSetResource,
	}
}

// DataSources returns nothing. There is nothing to query that a resource does
// not already expose as a state attribute, so the provider has no data sources.
func (p *accumulatorProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
