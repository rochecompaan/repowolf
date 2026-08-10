package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: release-server <directory>")
	}
	log.Fatal(http.ListenAndServe("127.0.0.1:8765", http.FileServer(http.Dir(os.Args[1]))))
}
