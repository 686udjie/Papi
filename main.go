// Package main provides a small HTTP API for extracting Pinterest media metadata.
package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/api/pin", Pin)

	log.Println("Running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
