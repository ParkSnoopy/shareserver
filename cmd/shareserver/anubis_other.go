//go:build !unix

package main

import (
	"log"

	"shareserver/internal/config"
)

func execAnubis(config.Config) {
	log.Fatal("the container Anubis launcher requires Unix")
}
