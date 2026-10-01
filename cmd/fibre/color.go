package main

import (
	"io"
	"os"
)

type tone int

const (
	toneBold tone = iota
	toneGood
	toneWarn
	toneBad
)

// useColor reports whether w is a terminal and NO_COLOR is not set.
func useColor(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func paint(color bool, t tone, s string) string {
	if !color {
		return s
	}
	code := map[tone]string{toneBold: "1", toneGood: "32", toneWarn: "33", toneBad: "31"}[t]
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
