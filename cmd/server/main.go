package main

import (
	"log"

	"github.com/cedrickchee/commitlog/internal/server"
)

func main() {
	srv := server.NewHTTPServer(":3000")
	log.Fatal(srv.ListenAndServe())
}
