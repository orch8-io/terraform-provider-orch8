package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/orch8-io/terraform-provider-orch8/internal/provider"
)

// Run "go generate" to regenerate docs/ with tfplugindocs.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name orch8

// version is set by goreleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/orch8-io/orch8",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
