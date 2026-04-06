// +build ignore

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// Simple test backend server for verifying the load balancer works.
func main() {
	port := "8081"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	name := "backend-" + port
	if len(os.Args) > 2 {
		name = os.Args[2]
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from %s! Method=%s Path=%s\n", name, r.Method, r.URL.Path)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})

	fmt.Printf("Test backend %s listening on :%s\n", name, port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
