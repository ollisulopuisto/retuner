package common

import (
	"strings"
	"net/url"
)

func StripChars(input string, chars []string) string {
	for _, c := range chars {
		input = strings.ReplaceAll(input, c, "")
	}
	return strings.TrimSpace(input)
}

func URLEncode(str string) string {
	return url.QueryEscape(str)
}
