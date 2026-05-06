package switcher

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

type CLI struct {
	Paths  Paths
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func Run(args []string, stdout, stderr io.Writer) error {
	p, err := DefaultPaths()
	if err != nil {
		return err
	}
	return CLI{Paths: p, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}.Run(args)
}

func RunWithPaths(args []string, p Paths, stdout, stderr io.Writer) error {
	return CLI{Paths: p, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}.Run(args)
}

func (c CLI) Run(args []string) error {
	p := c.Paths
	stdout := c.Stdout
	stderr := c.Stderr
	if c.Stdin == nil {
		c.Stdin = os.Stdin
	}
	if len(args) < 2 {
		return RunMenu(c)
	}

	switch args[1] {
	case "login":
		fs := flag.NewFlagSet("login", flag.ContinueOnError)
		fs.SetOutput(stderr)
		droidPath := fs.String("droid", "droid", "path to the droid executable")
		label := fs.String("label", "", "friendly label for the account")
		force := fs.Bool("force", false, "overwrite an existing saved account")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() > 1 {
			return errors.New("usage: droid-switcher login [account] [--label text] [--force] [--droid /path/to/droid]")
		}
		name := ""
		if fs.NArg() == 1 {
			name = fs.Arg(0)
		}
		return Login(p, name, *droidPath, *label, *force, stdout)
	case "switch":
		fs := flag.NewFlagSet("switch", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		var name string
		if fs.NArg() == 0 {
			var err error
			name, err = SelectAccount(p, c.Stdin, stdout)
			if err != nil {
				return err
			}
		} else if fs.NArg() == 1 {
			name = fs.Arg(0)
		} else {
			return errors.New("usage: droid-switcher switch [account]")
		}
		return SwitchAccount(p, name, stdout)
	case "select":
		fs := flag.NewFlagSet("select", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: droid-switcher select")
		}
		name, err := SelectAccount(p, c.Stdin, stdout)
		if err != nil {
			return err
		}
		return SwitchAccount(p, name, stdout)
	case "quota", "limits":
		fs := flag.NewFlagSet("quota", flag.ContinueOnError)
		fs.SetOutput(stderr)
		droidPath := fs.String("droid", "droid", "path to the droid executable")
		all := fs.Bool("all", false, "show quota for every saved account")
		raw := fs.Bool("raw", false, "include raw Droid /limits output")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() > 1 {
			return errors.New("usage: droid-switcher quota [account] [--all] [--raw] [--droid /path/to/droid]")
		}
		account := ""
		if fs.NArg() == 1 {
			account = fs.Arg(0)
		}
		return Quota(p, QuotaOptions{Account: account, All: *all, Raw: *raw, Droid: *droidPath, Stdin: c.Stdin}, stdout)
	case "save-current":
		fs := flag.NewFlagSet("save-current", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := fs.Bool("force", false, "overwrite an existing saved account")
		label := fs.String("label", "", "friendly label for the account")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() > 1 {
			return errors.New("usage: droid-switcher save-current [account] [--force] [--label text]")
		}
		name := ""
		if fs.NArg() == 1 {
			name = fs.Arg(0)
		}
		return SaveCurrent(p, name, SaveOptions{Force: *force, Label: *label}, stdout)
	case "sync-current":
		fs := flag.NewFlagSet("sync-current", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: droid-switcher sync-current")
		}
		return SyncCurrentAuthToSavedAccount(p, stdout)
	case "list":
		return printAccountList(p, stdout)
	case "current":
		return printCurrent(p, stdout)
	case "rename":
		fs := flag.NewFlagSet("rename", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return errors.New("usage: droid-switcher rename <old> <new>")
		}
		return RenameAccount(p, fs.Arg(0), fs.Arg(1), stdout)
	case "label":
		fs := flag.NewFlagSet("label", flag.ContinueOnError)
		fs.SetOutput(stderr)
		clear := fs.Bool("clear", false, "clear the label")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *clear {
			if fs.NArg() != 1 {
				return errors.New("usage: droid-switcher label <account> --clear")
			}
			return SetAccountLabel(p, fs.Arg(0), "", stdout)
		}
		if fs.NArg() != 2 {
			return errors.New("usage: droid-switcher label <account> <label>")
		}
		return SetAccountLabel(p, fs.Arg(0), fs.Arg(1), stdout)
	case "default":
		fs := flag.NewFlagSet("default", flag.ContinueOnError)
		fs.SetOutput(stderr)
		clear := fs.Bool("clear", false, "clear the default account")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *clear {
			if fs.NArg() != 0 {
				return errors.New("usage: droid-switcher default --clear")
			}
			return ClearDefaultAccount(p, stdout)
		}
		if fs.NArg() != 1 {
			return errors.New("usage: droid-switcher default <account>")
		}
		return SetDefaultAccount(p, fs.Arg(0), stdout)
	case "remove":
		fs := flag.NewFlagSet("remove", flag.ContinueOnError)
		fs.SetOutput(stderr)
		yes := fs.Bool("yes", false, "skip removal confirmation")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: droid-switcher remove <account> [--yes]")
		}
		if !*yes {
			if !writerIsTerminal(stdout) {
				return errors.New("remove requires --yes outside an interactive terminal")
			}
			confirm, err := promptLine(c.Stdin, stdout, fmt.Sprintf("Type %q to confirm removal: ", fs.Arg(0)))
			if err != nil {
				return err
			}
			if confirm != fs.Arg(0) {
				return errors.New("removal cancelled")
			}
		}
		return RemoveAccount(p, fs.Arg(0), stdout)
	case "version":
		if len(args) != 2 {
			return errors.New("usage: droid-switcher version")
		}
		fmt.Fprintln(stdout, version)
		return nil
	case "where":
		fmt.Fprintf(stdout, "store: %s\nfactory_home: %s\n", p.Store, p.FactoryHome)
		return nil
	case "help", "-h", "--help":
		PrintUsage(stdout)
		return nil
	case "menu":
		return RunMenu(c)
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  droid-switcher login [account]         Run Droid's own OAuth flow in an isolated account home
  droid-switcher save-current [account]  Save the current ~/.factory auth as an account
  droid-switcher sync-current            Sync the live ~/.factory auth back into the active saved account
  droid-switcher switch [account]        Make an account active for normal droid runs
  droid-switcher select                  Pick a saved account interactively
  droid-switcher quota [account]         Show Factory quota by running Droid /limits as that account
  droid-switcher quota --all             Compare quota across every saved account
  droid-switcher menu                    Open the interactive menu
  droid-switcher list                    List saved accounts
  droid-switcher current                 Show the active account marker
  droid-switcher rename <old> <new>      Rename a saved account
  droid-switcher label <account> <text>  Set a friendly label
  droid-switcher default <account>       Set the default account
  droid-switcher remove <account>        Delete a saved account
  droid-switcher version                 Print the build version
  droid-switcher where                   Show switcher storage paths

The switcher stores account homes under ~/.droid-switcher/accounts and only
replaces Droid's auth.v2.file/auth.v2.key in ~/.factory when switching.

Use --raw with quota if Factory changes the /limits display and you need the
original Droid output.`)
}

func printAccountList(p Paths, stdout io.Writer) error {
	accounts, err := ListAccounts(p)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Fprintln(stdout, "No accounts saved.")
		return nil
	}
	for _, account := range accounts {
		marker := " "
		switch {
		case account.Active && account.Default:
			marker = "*D"
		case account.Active:
			marker = "* "
		case account.Default:
			marker = "D "
		}
		status := "missing-auth"
		if account.Ready {
			status = "ready"
		}
		fmt.Fprintf(stdout, "%-2s %-36s %s\n", marker, displayAccount(account), status)
	}
	return nil
}

func printCurrent(p Paths, stdout io.Writer) error {
	name, ok, err := CurrentAccount(p)
	if err != nil {
		return err
	}
	if !ok {
		defaultName, defaultOK, err := CurrentDefaultAccount(p)
		if err != nil {
			return err
		}
		if !defaultOK {
			fmt.Fprintln(stdout, "No active account marker.")
			return nil
		}
		accounts, _ := ListAccounts(p)
		for _, account := range accounts {
			if account.Name == defaultName {
				fmt.Fprintf(stdout, "default: %s\n", displayAccount(account))
				return nil
			}
		}
		fmt.Fprintf(stdout, "default: %s\n", defaultName)
		return nil
	}
	accounts, _ := ListAccounts(p)
	for _, account := range accounts {
		if account.Name == name {
			fmt.Fprintln(stdout, displayAccount(account))
			return nil
		}
	}
	fmt.Fprintln(stdout, name)
	return nil
}
