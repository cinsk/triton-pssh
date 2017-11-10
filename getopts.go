package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	NO_ARGUMENT       bool = false
	ARGUMENT_REQUIRED      = true
)

type OptionSpec struct {
	Option           rune
	LongOption       string
	ArgumentRequired bool
}

type Option struct {
	Option     rune
	LongOption string
	Argument   string
}

type GetoptContext struct {
	Args    []string // read only copy of os.Args
	Options []OptionSpec
	done    bool
	sopts   []rune
}

func (c *GetoptContext) Arguments() []string {
	return c.Args
}

// -ascd 4 --hello=xxx

func (c *GetoptContext) Getopt() (*Option, error) {
	if c.sopts == nil {
		if c.Args == nil {
			c.Args = os.Args[1:]
		}
		c.sopts = []rune{}
	}

	if c.done {
		return nil, nil
	}

	if len(c.sopts) > 0 { // parse short options
		opt := c.sopts[0]
		c.sopts = c.sopts[1:]

		for _, spec := range c.Options {
			if opt == spec.Option {
				if spec.ArgumentRequired {
					if len(c.sopts) == 0 {
						if len(c.Args) == 0 {
							return nil, fmt.Errorf("option %c requires an argument", opt)
						} else {
							arg := c.Args[0]
							c.Args = c.Args[1:]

							if arg != "--" {
								return &Option{Option: spec.Option, LongOption: spec.LongOption, Argument: arg}, nil
							} else {
								c.done = true
								return nil, fmt.Errorf("option %c requires an argument", opt)
							}
						}
					} else {
						return nil, fmt.Errorf("option %s requires an argument", opt)
					}
				} else {
					return &Option{Option: spec.Option, LongOption: spec.LongOption}, nil
				}
			}
		}
		return nil, fmt.Errorf("unrecognized option -- %c", opt)
	}

	if len(c.Args) == 0 {
		c.done = true
		return nil, nil
	}

	word := c.Args[0]

	if word == "--" {
		c.Args = c.Args[1:]
		c.done = true
		return nil, nil
	}

	if len(word) < 2 {
		c.done = true
		return nil, nil
	}

	if word[:2] == "--" { // long option
		tokens := strings.SplitN(word[2:], "=", 2)
		if len(tokens) < 1 {
			panic("word must have at least 1 token")
		}
		c.Args = c.Args[1:]

		for _, spec := range c.Options {
			if tokens[0] == spec.LongOption {
				if spec.ArgumentRequired {
					if len(tokens) == 1 {
						return nil, fmt.Errorf("option %s requires an argument", tokens[0])
					}
					return &Option{Option: spec.Option, LongOption: spec.LongOption, Argument: tokens[1]}, nil
				} else {
					return &Option{Option: spec.Option, LongOption: spec.LongOption}, nil
				}
			}
		}
		return nil, fmt.Errorf("unrecognized option -- %s", tokens[0])

	} else if word[:1] == "-" { // short option
		c.Args = c.Args[1:]
		c.sopts = []rune(word[1:])
		return c.Getopt()
	} else {
		c.done = true
		return nil, nil
	}

	return nil, nil
}

func getopts_main() {
	options := []OptionSpec{
		{'c', "", false},
		{'d', "", false},
		{0, "help", false},
		{0, "version", false},
		{'o', "output", true},
	}

	context := GetoptContext{Options: options, Args: os.Args[1:]}
	for {
		opt, err := context.Getopt()

		if err != nil {
			fmt.Printf("error: %s\n", err)
			break
		}

		if opt == nil {
			break
		}
		switch opt.Option {
		default:
			if opt.Argument != "" {
				fmt.Printf("option %c[%s] found -- argument = %v\n", opt.Option, opt.LongOption, opt.Argument)
			} else {
				fmt.Printf("option %c[%s] found\n", opt.Option, opt.LongOption)
			}
		}

	}
	fmt.Printf("args: %v\n", context.Arguments())
}
