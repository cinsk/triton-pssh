package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	l "github.com/cinsk/triton-pssh/log"
)

const OPTION_FILENAME = "triton-pssh.options"

func CreateInitFile(filepath string) {
	contents := fmt.Sprintf(`# Generated by triton-pssh version %s at %s
# each non-empty line must contains exact one word
`,
		VERSION_STRING, time.Now().Format(time.UnixDate))
	ioutil.WriteFile(filepath, []byte(contents), 0600)
}

func OptionsFromInitFile() []string {
	initfile := path.Join(HomeDirectory, TSSH_ROOT, OPTION_FILENAME)
	file, err := os.Open(initfile)

	if err != nil {
		if os.IsNotExist(err) {
			CreateInitFile(initfile)
		}
		l.Warn("cannot open init file, %v", initfile)
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var options []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) != 0 && line[0] != '#' {
			if line == ":::" {
				l.ErrQuit(1, "%s: \":::\" must not be part of options", OPTION_FILENAME)
			}
			options = append(options, line)
		}
	}
	return options
}
