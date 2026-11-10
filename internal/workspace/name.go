package workspace

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// normalizeName trims and validates a user-supplied workspace name.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w：名称不能为空", ErrInvalidName)
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", fmt.Errorf("%w：名称不能超过 %d 个字符", ErrInvalidName, maxNameRunes)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w：名称包含控制字符", ErrInvalidName)
		}
	}
	return name, nil
}
