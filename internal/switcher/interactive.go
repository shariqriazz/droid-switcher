package switcher

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

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
