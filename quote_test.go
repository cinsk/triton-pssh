package main

import "testing"

func TestQuote_ExpandPlaceHolder(env *testing.T) {
	cases := [][]string{
		{"hello there", "{}:"},
		{"{}:", "hello there"},
		{"hello", "there", "{}:/src/the dir", "dest/the dir"},
		{"hello", "there", "src/the dir", "{}:dest/the dir"},
	}

	for i, src := range cases {
		cmd, err := ExpandPlaceholder(src, "REMOTE")
		if err != nil {
			env.Errorf("testcase#%d: unexpected error: %v", i, err)
		}
		env.Logf("cmd: %s", cmd)
	}

}

func TestQuote_ExpandPlaceHolder_Expect_Failure(env *testing.T) {
	cases := [][]string{
		{},
		{"hello there"},
		{"hello", "there", "/src/the dir", "dest/the dir"},
		{"hello", "there", "src/the dir", "dest/the dir"},
	}

	for i, src := range cases {
		cmd, err := ExpandPlaceholder(src, "REMOTE")
		if err == nil {
			env.Errorf("testcase#%d: expected error, but succeeded: %s", i, cmd)
		}
	}

}
