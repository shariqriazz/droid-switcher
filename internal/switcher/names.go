package switcher

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func CleanAccountName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", errors.New("account name is required")
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("account name %q may only contain letters, numbers, dot, dash, and underscore", name)
	}
	return name, nil
}

func ResolveAccountName(p Paths, requested string) (string, bool, error) {
	if strings.TrimSpace(requested) != "" {
		name, err := CleanAccountName(requested)
		return name, false, err
	}
	for i := 0; i < 100; i++ {
		candidate := fmt.Sprintf("account-%s-%s", time.Now().Format("20060102-150405"), randomSuffix()[:6])
		if _, err := osStat(p.AccountFactoryHome(candidate)); errors.Is(err, errNotExist()) {
			return candidate, true, nil
		} else if err != nil && !errors.Is(err, errNotExist()) {
			return "", false, err
		}
	}
	return "", false, errors.New("failed to generate a unique account name")
}
