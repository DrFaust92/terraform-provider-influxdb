package main

// Format the examples and generate the docs/ directory.
//go:generate terraform fmt -recursive ./examples/
//go:generate go tool tfplugindocs generate --provider-name influxdb

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/terraform-providers/terraform-provider-influxdb/influxdb"
)

// version is set by GoReleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), influxdb.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/DrFaust92/influxdb",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
