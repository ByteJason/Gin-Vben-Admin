package main

import (
	"log"
	"os"

	"example.com/gin-vben-admin/server/internal/bootstrap"
)

func main() {
	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("api listening on %s", addr)
	if err := bootstrap.NewHTTPServer(addr).ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
