package switcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SelectAccount prompts for a ready account by number, id, or unique label.
func SelectAccount(p Paths, stdin io.Reader, stdout io.Writer) (string, error) {
	accounts, err := ListAccounts(p)
	if err != nil {
		return "", err
	}
	var ready []AccountStatus
	for _, account := range accounts {
		if account.Ready {
			ready = append(ready, account)
		}
	}
	if len(ready) == 0 {
		return "", errors.New("no ready accounts saved; run droid-switcher login <account> first")
	}
	if len(ready) == 1 {
		return ready[0].Name, nil
	}
	fmt.Fprintln(stdout, "Select a Droid account:")
	for i, account := range ready {
		marker := " "
		switch {
		case account.Active && account.Default:
			marker = "*D"
		case account.Active:
			marker = "* "
		case account.Default:
			marker = "D "
		}
		fmt.Fprintf(stdout, "  %d. %s %s\n", i+1, marker, displayAccount(account))
	}
	fmt.Fprint(stdout, "Account number or name: ")

	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", errors.New("no selection provided")
	}
	choice := strings.TrimSpace(scanner.Text())
	if idx, err := strconv.Atoi(choice); err == nil {
		if idx < 1 || idx > len(ready) {
			return "", fmt.Errorf("selection %d is out of range", idx)
		}
		return ready[idx-1].Name, nil
	}
	name, err := CleanAccountName(choice)
	if err == nil {
		for _, account := range ready {
			if account.Name == name {
				return name, nil
			}
		}
	}
	matches := matchAccountsByLabel(ready, choice)
	if len(matches) == 1 {
		return matches[0].Name, nil
	}
	if len(matches) > 1 {
		var labels []string
		for _, account := range matches {
			labels = append(labels, displayAccount(account))
		}
		return "", fmt.Errorf("label %q is ambiguous: %s", choice, strings.Join(labels, ", "))
	}
	if err != nil {
		return "", err
	}
	for _, account := range ready {
		if account.Name == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("account %q is not saved or is missing auth files", name)
}

func matchAccountsByLabel(accounts []AccountStatus, input string) []AccountStatus {
	var matches []AccountStatus
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return nil
	}
	for _, account := range accounts {
		if strings.ToLower(strings.TrimSpace(account.Label)) == input {
			matches = append(matches, account)
		}
	}
	return matches
}
