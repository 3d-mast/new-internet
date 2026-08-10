//go:build !windows

package main

import "fmt"

func showFatalError(message string) { fmt.Println(message) }
func showWarning(message string)    { fmt.Println(message) }
