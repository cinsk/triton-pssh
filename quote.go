package main

import (
	"fmt"
	"regexp"

	shellquote "github.com/kballard/go-shellquote"
)

var regexpHost = regexp.MustCompile("\\{\\}")

func ExpandPlaceholder(command []string, repl string) (string, error) {
	var replaced []string
	var placeholderDetected bool

	for _, word := range command {
		if regexpHost.FindStringIndex(word) != nil {
			placeholderDetected = true
		}
		replaced = append(replaced, regexpHost.ReplaceAllString(word, repl))
	}
	if !placeholderDetected {
		return "", fmt.Errorf("placeholder {} not found")
	}
	return shellquote.Join(replaced...), nil
}
