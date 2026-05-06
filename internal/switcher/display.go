package switcher

import "fmt"

func displayAccount(account AccountStatus) string {
	if account.Label == "" {
		return account.Name
	}
	return fmt.Sprintf("%s (%s)", account.Label, account.Name)
}
