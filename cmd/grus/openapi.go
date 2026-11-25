package main

import (
	"io"

	"github.com/OneLiteFeatherNET/grus/internal/api"
)

// runOpenAPI prints the generated OpenAPI 3.1 document to w.
func runOpenAPI(w io.Writer) error {
	doc, err := api.OpenAPI()
	if err != nil {
		return err
	}
	_, err = w.Write(doc)
	return err
}
