package main

import "testing"

const (
	__OPTION_HELP = iota
	__OPTION_VERSION
	__OPTION_URL
	__OPTION_BASTION_PORT
	__OPTION_DEFAULT_USER
	__OPTION_PASSWORD
	__OPTION_NOCACHE
)

var __Options = []OptionSpec{
	{__OPTION_HELP, "help", NO_ARGUMENT},
	{__OPTION_VERSION, "version", NO_ARGUMENT},
	{'k', "keyid", ARGUMENT_REQUIRED},
	// {'K', "keyfile", ARGUMENT_REQUIRED},
	{__OPTION_URL, "url", ARGUMENT_REQUIRED},
	{'u', "user", ARGUMENT_REQUIRED},
	{'P', "port", ARGUMENT_REQUIRED},
	{'U', "bastion-user", ARGUMENT_REQUIRED},
	{'b', "bastion", ARGUMENT_REQUIRED},
	{__OPTION_BASTION_PORT, "bastion-port", ARGUMENT_REQUIRED},
	{'T', "timeout", ARGUMENT_REQUIRED},
	{'t', "deadline", ARGUMENT_REQUIRED},
	{'p', "parallel", ARGUMENT_REQUIRED},
	{'i', "inline", NO_ARGUMENT},
	{'o', "outdir", ARGUMENT_REQUIRED},
	{'e', "errdir", ARGUMENT_REQUIRED},
	{__OPTION_DEFAULT_USER, "default-user", ARGUMENT_REQUIRED},
	{__OPTION_PASSWORD, "password", NO_ARGUMENT},
	{'I', "identity", ARGUMENT_REQUIRED},
	{__OPTION_NOCACHE, "no-cache", NO_ARGUMENT},
}

func TestGetoptEmptyArguments(env *testing.T) {
	args := []string{}

	context := GetoptContext{Args: args, Options: __Options}

	opt, err := context.Getopt()

	if err != nil {
		env.Errorf("Getopt failed: %s", err)
	}

	if opt != nil {
		env.Errorf("Getopt wrongly parsed option: %v", opt)
	}

	remaining := context.Arguments()
	if len(remaining) != 0 {
		env.Errorf("Getopt wrongly produced argument: %v", remaining)
	}

}

func TestGetoptNoOptionSomeArguments(env *testing.T) {
	args := []string{"hello", "world"}

	context := GetoptContext{Args: args, Options: __Options}

	opt, err := context.Getopt()

	if err != nil {
		env.Errorf("Getopt failed: %s", err)
	}

	if opt != nil {
		env.Errorf("Getopt wrongly parsed option: %v", opt)
	}

	remaining := context.Arguments()
	if len(remaining) != 2 {
		env.Errorf("Getopt wrongly produced different number of arguments: len(remaining)[%d] != len(args)[%d]", len(remaining), len(args))
	}

	for i := 0; i < len(args); i++ {
		if remaining[i] != args[i] {
			env.Errorf("Getopt wrongly produced argument: remaining[%d](%v) != args[%d](%v)", i, remaining[i], i, args[i])
		}
	}

}

func TestGetoptNoOptionDelimeterSomeArguments(env *testing.T) {
	args := []string{"--", "hello", "world"}

	context := GetoptContext{Args: args, Options: __Options}

	opt, err := context.Getopt()

	if err != nil {
		env.Errorf("Getopt failed: %s", err)
	}

	if opt != nil {
		env.Errorf("Getopt wrongly parsed option: %v", opt)
	}

	remaining := context.Arguments()
	if len(remaining) != 2 {
		env.Errorf("Getopt wrongly produced different number of arguments: len(remaining)[%d] != len(args)[%d] - 1", len(remaining), len(args))
	}

	for i := 0; i < len(remaining); i++ {
		if remaining[i] != args[i+1] {
			env.Errorf("Getopt wrongly produced argument: remaining[%d](%v) != args[%d](%v)", i, remaining[i], i, args[i])
		}
	}

	if env.Failed() {
		env.Errorf("args = %v\n", args)
		env.Errorf("remains = %v\n", remaining)
	}

}
