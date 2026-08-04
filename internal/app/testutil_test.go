package app

import (
	"io"
	"log"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }
