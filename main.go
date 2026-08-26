// Command terraform-provider-oncall is the plugin server entrypoint. Install
// it via `go install .` (or `dev_overrides` for local development, see
// README.md) rather than running it directly.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/observiply/terraform-provider-oncall/internal/provider"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate

// version is overwritten at build time via:
//
//	go build -ldflags "-X main.version=$(VERSION)"
//
// See .goreleaser.yml.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/observiply/oncall",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
