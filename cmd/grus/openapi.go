package main

import (
	"os"

	"github.com/OneLiteFeatherNET/grus/internal/api"
)

// runOpenAPI prints the generated OpenAPI 3.1 document to stdout.
func runOpenAPI() error {
	doc, err := api.OpenAPI()
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(doc)
	return err
}
