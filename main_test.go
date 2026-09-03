package main

import (
	"context"
	"strings"
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"

	oncallprovider "github.com/observiply/terraform-provider-oncall/internal/provider"
)

const providerTypeName = "oncall"

// TestProviderAddressMatchesTypeName guards the wiring between main() and the
// provider package: providerserver.Serve is handed providerAddress, and the
// last segment of that address must equal the TypeName the provider reports,
// or `terraform init` resolves the wrong source address.
func TestProviderAddressMatchesTypeName(t *testing.T) {
	if want := "registry.terraform.io/observiply/oncall"; providerAddress != want {
		t.Fatalf("providerAddress = %q, want %q", providerAddress, want)
	}

	last := providerAddress[strings.LastIndexByte(providerAddress, '/')+1:]

	metaResp := &fwprovider.MetadataResponse{}
	oncallprovider.New(version)().Metadata(context.Background(), fwprovider.MetadataRequest{}, metaResp)

	if metaResp.TypeName != last {
		t.Fatalf("provider TypeName %q does not match providerAddress last segment %q", metaResp.TypeName, last)
	}
	if metaResp.TypeName != providerTypeName {
		t.Fatalf("provider TypeName = %q, want %q", metaResp.TypeName, providerTypeName)
	}
}

// TestProviderVersionDefault documents that an un-stamped build (no -ldflags
// -X main.version=...) reports "dev", which the User-Agent header and oncall's
// access logs rely on to distinguish local builds from releases.
func TestProviderVersionDefault(t *testing.T) {
	if version != "dev" {
		t.Fatalf("default version = %q, want %q", version, "dev")
	}

	metaResp := &fwprovider.MetadataResponse{}
	oncallprovider.New(version)().Metadata(context.Background(), fwprovider.MetadataRequest{}, metaResp)
	if metaResp.Version != version {
		t.Fatalf("provider reported version %q, want %q", metaResp.Version, version)
	}
}

// TestProviderSchema validates the provider's own configuration schema.
func TestProviderSchema(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwprovider.SchemaResponse{}
	oncallprovider.New(version)().Schema(ctx, fwprovider.SchemaRequest{}, schemaResp)

	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("provider schema diagnostics: %v", schemaResp.Diagnostics)
	}
	if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("provider schema ValidateImplementation: %v", diags)
	}
}

// TestResourceSchemas instantiates every registered resource, checks its
// schema compiles cleanly, and asserts the TypeNames are unique and all
// carry the provider prefix — the failure mode when a resource is added to
// provider.Resources() with a copy-pasted Metadata method.
func TestResourceSchemas(t *testing.T) {
	ctx := context.Background()
	factories := oncallprovider.New(version)().Resources(ctx)
	if len(factories) == 0 {
		t.Fatal("provider registered no resources")
	}

	seen := make(map[string]struct{}, len(factories))
	for _, factory := range factories {
		r := factory()

		metaResp := &fwresource.MetadataResponse{}
		r.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: providerTypeName}, metaResp)
		name := metaResp.TypeName

		if !strings.HasPrefix(name, providerTypeName+"_") {
			t.Errorf("resource TypeName %q is missing the %q prefix", name, providerTypeName+"_")
		}
		if _, dup := seen[name]; dup {
			t.Errorf("duplicate resource TypeName %q", name)
		}
		seen[name] = struct{}{}

		schemaResp := &fwresource.SchemaResponse{}
		r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Errorf("resource %q schema diagnostics: %v", name, schemaResp.Diagnostics)
			continue
		}
		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("resource %q schema ValidateImplementation: %v", name, diags)
		}
	}
}

// TestDataSourceSchemas is TestResourceSchemas for the read-only data sources.
func TestDataSourceSchemas(t *testing.T) {
	ctx := context.Background()
	factories := oncallprovider.New(version)().DataSources(ctx)
	if len(factories) == 0 {
		t.Fatal("provider registered no data sources")
	}

	seen := make(map[string]struct{}, len(factories))
	for _, factory := range factories {
		d := factory()

		metaResp := &fwdatasource.MetadataResponse{}
		d.Metadata(ctx, fwdatasource.MetadataRequest{ProviderTypeName: providerTypeName}, metaResp)
		name := metaResp.TypeName

		if !strings.HasPrefix(name, providerTypeName+"_") {
			t.Errorf("data source TypeName %q is missing the %q prefix", name, providerTypeName+"_")
		}
		if _, dup := seen[name]; dup {
			t.Errorf("duplicate data source TypeName %q", name)
		}
		seen[name] = struct{}{}

		schemaResp := &fwdatasource.SchemaResponse{}
		d.Schema(ctx, fwdatasource.SchemaRequest{}, schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Errorf("data source %q schema diagnostics: %v", name, schemaResp.Diagnostics)
			continue
		}
		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("data source %q schema ValidateImplementation: %v", name, diags)
		}
	}
}
