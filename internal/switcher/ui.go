package switcher

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func promptLine(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func promptMenuChoice(stdin io.Reader, stdout io.Writer, title string, options []string) (int, error) {
	fmt.Fprintln(stdout, title)
	for i, option := range options {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, option)
	}
	line, err := promptLine(stdin, stdout, "Choose an option: ")
	if err != nil {
		return 0, err
	}
	idx, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("invalid choice %q", line)
	}
	if idx < 1 || idx > len(options) {
		return 0, fmt.Errorf("choice %d is out of range", idx)
	}
	return idx - 1, nil
}

func printBanner(stdout io.Writer) {
	fmt.Fprintln(stdout, "Droid Switcher")
	fmt.Fprintln(stdout, "--------------")
	fmt.Fprintln(stdout, "Multi-account login switching and /limits quota overview for Factory Droid.")
	fmt.Fprintln(stdout)
}

func RunMenu(c CLI) error {
	printBanner(c.Stdout)
	accounts, err := ListAccounts(c.Paths)
	if err != nil {
		return err
	}
	if len(accounts) > 0 {
		fmt.Fprintln(c.Stdout, "Saved accounts:")
		if err := printAccountList(c.Paths, c.Stdout); err != nil {
			return err
		}
		fmt.Fprintln(c.Stdout)
	}
	choice, err := promptMenuChoice(c.Stdin, c.Stdout, "What do you want to do?", []string{
		"Switch account",
		"Show quota for current/default account",
		"Compare quota across all accounts",
		"Add account with Droid login",
		"Save current ~/.factory login",
		"Set default account",
		"Rename account",
		"Set account label",
		"Remove account",
		"Show help",
	})
	if err != nil {
		return err
	}
	switch choice {
	case 0:
		return c.Run([]string{"droid-switcher", "select"})
	case 1:
		return c.Run([]string{"droid-switcher", "quota"})
	case 2:
		return c.Run([]string{"droid-switcher", "quota", "--all"})
	case 3:
		name, err := promptLine(c.Stdin, c.Stdout, "Account name (blank = generate): ")
		if err != nil {
			return err
		}
		label, err := promptLine(c.Stdin, c.Stdout, "Friendly label (optional): ")
		if err != nil {
			return err
		}
		args := []string{"droid-switcher", "login"}
		if label != "" {
			args = append(args, "--label", label)
		}
		if name != "" {
			args = append(args, name)
		}
		return c.Run(args)
	case 4:
		name, err := promptLine(c.Stdin, c.Stdout, "Save as account name (blank = generate): ")
		if err != nil {
			return err
		}
		label, err := promptLine(c.Stdin, c.Stdout, "Friendly label (optional): ")
		if err != nil {
			return err
		}
		args := []string{"droid-switcher", "save-current"}
		if label != "" {
			args = append(args, "--label", label)
		}
		if name != "" {
			args = append(args, name)
		}
		return c.Run(args)
	case 5:
		name, err := SelectAccount(c.Paths, c.Stdin, c.Stdout)
		if err != nil {
			return err
		}
		return c.Run([]string{"droid-switcher", "default", name})
	case 6:
		name, err := SelectAccount(c.Paths, c.Stdin, c.Stdout)
		if err != nil {
			return err
		}
		newName, err := promptLine(c.Stdin, c.Stdout, "New account name: ")
		if err != nil {
			return err
		}
		return c.Run([]string{"droid-switcher", "rename", name, newName})
	case 7:
		name, err := SelectAccount(c.Paths, c.Stdin, c.Stdout)
		if err != nil {
			return err
		}
		label, err := promptLine(c.Stdin, c.Stdout, "New label (blank clears it): ")
		if err != nil {
			return err
		}
		if label == "" {
			return c.Run([]string{"droid-switcher", "label", name, "--clear"})
		}
		return c.Run([]string{"droid-switcher", "label", name, label})
	case 8:
		name, err := SelectAccount(c.Paths, c.Stdin, c.Stdout)
		if err != nil {
			return err
		}
		confirm, err := promptLine(c.Stdin, c.Stdout, fmt.Sprintf("Type %q to confirm removal: ", name))
		if err != nil {
			return err
		}
		if confirm != name {
			fmt.Fprintln(c.Stdout, "Removal cancelled.")
			return nil
		}
		return c.Run([]string{"droid-switcher", "remove", name})
	default:
		PrintUsage(c.Stdout)
		return nil
	}
}
