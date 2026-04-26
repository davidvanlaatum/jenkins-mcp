package validation

import (
	"errors"
	"strconv"
	"strings"
)

func JobPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("job path is required")
	}
	return nil
}
func BuildNumber(n int) error {
	if n <= 0 {
		return errors.New("build number must be positive")
	}
	return nil
}
func ParseBuildNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, BuildNumber(n)
}
