package switcher

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// CLI holds the filesystem and I/O dependencies used by command handlers.
type CLI struct {
	Paths  Paths
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes the CLI against the current user's default storage paths.
func Run(args []string, stdout, stderr io.Writer) error {
	p, err := DefaultPaths()
	if err != nil {
		return err
	}
	return CLI{Paths: p, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}.Run(args)
}

// RunWithPaths executes the CLI with explicit paths for embedding and tests.
func RunWithPaths(args []string, p Paths, stdout, stderr io.Writer) error {
	return CLI{Paths: p, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}.Run(args)
}

// Run routes one command invocation and returns usage or runtime failures.
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
	case "login", "add":
		fs := flag.NewFlagSet("login", flag.ContinueOnError)
		fs.SetOutput(stderr)
		droidPath := stringFlag(fs, "droid", "d", "droid", "path to the droid executable")
		label := stringFlag(fs, "label", "l", "", "friendly label for the account")
		force := boolFlag(fs, "force", "f", false, "overwrite an existing saved account")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) > 1 {
			return errors.New("usage: droid-switcher login [account] [--label text|-l text] [--force|-f] [--droid|-d /path/to/droid]")
		}
		name := ""
		if len(positionals) == 1 {
			name = positionals[0]
		}
		return Login(p, name, *droidPath, *label, *force, stdout)
	case "switch", "sw", "s":
		fs := flag.NewFlagSet("switch", flag.ContinueOnError)
		fs.SetOutput(stderr)
		noShare := boolFlag(fs, "no-share", "", false, "do not unlock sessions across accounts")
		_ = boolFlag(fs, "share-sessions", "s", true, "unlock sessions across accounts (default true)")
		_ = fs.Bool("share", true, "alias for --share-sessions")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		var name string
		if len(positionals) == 0 {
			var err error
			name, err = SelectAccount(p, c.Stdin, stdout)
			if err != nil {
				return err
			}
		} else if len(positionals) == 1 {
			name = positionals[0]
		} else {
			return errors.New("usage: droid-switcher switch [account] [--no-share]")
		}
		return SwitchAccountWithOptions(p, name, SwitchOptions{NoShare: *noShare}, stdout)
	case "select", "sel":
		fs := flag.NewFlagSet("select", flag.ContinueOnError)
		fs.SetOutput(stderr)
		noShare := boolFlag(fs, "no-share", "", false, "do not unlock sessions across accounts")
		_ = boolFlag(fs, "share-sessions", "s", true, "unlock sessions across accounts (default true)")
		_ = fs.Bool("share", true, "alias for --share-sessions")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) != 0 {
			return errors.New("usage: droid-switcher select [--no-share]")
		}
		name, err := SelectAccount(p, c.Stdin, stdout)
		if err != nil {
			return err
		}
		return SwitchAccountWithOptions(p, name, SwitchOptions{NoShare: *noShare}, stdout)
	case "quota", "limits", "q":
		fs := flag.NewFlagSet("quota", flag.ContinueOnError)
		fs.SetOutput(stderr)
		droidPath := stringFlag(fs, "droid", "d", "droid", "accepted for compatibility; quota uses saved auth directly")
		all := boolFlag(fs, "all", "a", false, "show quota for every saved account")
		raw := boolFlag(fs, "raw", "r", false, "include raw Factory limits response")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) > 1 {
			return errors.New("usage: droid-switcher quota [account] [--all|-a] [--raw|-r]")
		}
		account := ""
		if len(positionals) == 1 {
			account = positionals[0]
		}
		return Quota(p, QuotaOptions{Account: account, All: *all, Raw: *raw, Droid: *droidPath, Stdin: c.Stdin}, stdout)
	case "save-current", "save", "sc":
		fs := flag.NewFlagSet("save-current", flag.ContinueOnError)
		fs.SetOutput(stderr)
		force := boolFlag(fs, "force", "f", false, "overwrite an existing saved account")
		label := stringFlag(fs, "label", "l", "", "friendly label for the account")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) > 1 {
			return errors.New("usage: droid-switcher save-current [account] [--force|-f] [--label|-l text]")
		}
		name := ""
		if len(positionals) == 1 {
			name = positionals[0]
		}
		return SaveCurrent(p, name, SaveOptions{Force: *force, Label: *label}, stdout)
	case "sync-current", "sync":
		fs := flag.NewFlagSet("sync-current", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: droid-switcher sync-current")
		}
		return SyncCurrentAuthToSavedAccount(p, stdout)
	case "doctor", "doc":
		fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
		fs.SetOutput(stderr)
		heal := boolFlag(fs, "heal", "", false, "rewrite the OS keyring to the single key that decrypts the live home")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) != 0 {
			return errors.New("usage: droid-switcher doctor [--heal]")
		}
		return Doctor(p, DoctorOptions{Heal: *heal}, stdout)
	case "list", "ls":
		return printAccountList(p, stdout)
	case "current", "cur":
		return printCurrent(p, stdout)
	case "rename", "mv":
		fs := flag.NewFlagSet("rename", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return errors.New("usage: droid-switcher rename <old> <new>")
		}
		return RenameAccount(p, fs.Arg(0), fs.Arg(1), stdout)
	case "label", "lbl":
		fs := flag.NewFlagSet("label", flag.ContinueOnError)
		fs.SetOutput(stderr)
		clearFlag := boolFlag(fs, "clear", "c", false, "clear the label")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if *clearFlag {
			if len(positionals) != 1 {
				return errors.New("usage: droid-switcher label <account> --clear|-c")
			}
			return SetAccountLabel(p, positionals[0], "", stdout)
		}
		if len(positionals) != 2 {
			return errors.New("usage: droid-switcher label <account> <label>")
		}
		return SetAccountLabel(p, positionals[0], positionals[1], stdout)
	case "default", "def":
		fs := flag.NewFlagSet("default", flag.ContinueOnError)
		fs.SetOutput(stderr)
		clearFlag := boolFlag(fs, "clear", "c", false, "clear the default account")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if *clearFlag {
			if len(positionals) != 0 {
				return errors.New("usage: droid-switcher default --clear|-c")
			}
			return ClearDefaultAccount(p, stdout)
		}
		if len(positionals) != 1 {
			return errors.New("usage: droid-switcher default <account>")
		}
		return SetDefaultAccount(p, positionals[0], stdout)
	case "remove", "rm":
		fs := flag.NewFlagSet("remove", flag.ContinueOnError)
		fs.SetOutput(stderr)
		yes := boolFlag(fs, "yes", "y", false, "skip removal confirmation")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: droid-switcher remove <account> [--yes|-y]")
		}
		if !*yes {
			if !writerIsTerminal(stdout) {
				return errors.New("remove requires --yes outside an interactive terminal")
			}
			confirm, err := promptLine(c.Stdin, stdout, fmt.Sprintf("Type %q to confirm removal: ", positionals[0]))
			if err != nil {
				return err
			}
			if confirm != positionals[0] {
				return errors.New("removal cancelled")
			}
		}
		return RemoveAccount(p, positionals[0], stdout)
	case "share-sessions", "--share-sessions", "share", "--share", "-s", "unbind-sessions", "--unbind-sessions", "unbind", "--unbind":
		fs := flag.NewFlagSet("share-sessions", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dryRun := boolFlag(fs, "dry-run", "n", false, "preview session updates without modifying files")
		list := boolFlag(fs, "list", "l", false, "list sessions and their organization binding")
		adopt := stringFlag(fs, "adopt", "", "", "adopt sessions into this organization ID instead of removing lock")
		positionals, err := parseInterspersed(fs, args[2:])
		if err != nil {
			return err
		}
		return ShareSessions(p, ShareOptions{
			SessionIDs: positionals,
			DryRun:     *dryRun,
			List:       *list,
			AdoptOrg:   *adopt,
		}, stdout)
	case "version", "v":
		if len(args) != 2 {
			return errors.New("usage: droid-switcher version")
		}
		fmt.Fprintln(stdout, version)
		return nil
	case "where", "w":
		fmt.Fprintf(stdout, "store: %s\nfactory_home: %s\n", p.Store, p.FactoryHome)
		return nil
	case "help", "h", "-h", "--help":
		PrintUsage(stdout)
		return nil
	case "menu", "m":
		return RunMenu(c)
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func stringFlag(fs *flag.FlagSet, longName, shortName, defaultValue, usage string) *string {
	value := defaultValue
	fs.StringVar(&value, longName, defaultValue, usage)
	if shortName != "" {
		fs.StringVar(&value, shortName, defaultValue, usage+" (shorthand)")
	}
	return &value
}

func boolFlag(fs *flag.FlagSet, longName, shortName string, defaultValue bool, usage string) *bool {
	value := defaultValue
	fs.BoolVar(&value, longName, defaultValue, usage)
	if shortName != "" {
		fs.BoolVar(&value, shortName, defaultValue, usage+" (shorthand)")
	}
	return &value
}

type boolFlagValue interface {
	IsBoolFlag() bool
}

func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		nameValue := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(nameValue, "=")
		f := fs.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("flag provided but not defined: -%s", name)
		}
		flagArgs = append(flagArgs, arg)
		if hasValue || flagIsBool(f) {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag needs an argument: -%s", name)
		}
		i++
		flagArgs = append(flagArgs, args[i])
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positionals, nil
}

func flagIsBool(f *flag.Flag) bool {
	value, ok := f.Value.(boolFlagValue)
	return ok && value.IsBoolFlag()
}

// PrintUsage writes the supported command and flag surface.
func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  droid-switcher login|add [account]          Run Droid's own OAuth flow in an isolated account home
  droid-switcher save-current|save|sc [acct]  Save the current ~/.factory auth as an account
  droid-switcher sync-current|sync            Sync the live ~/.factory auth back into the active saved account
  droid-switcher doctor|doc [--heal]          Diagnose Droid OS keyring state; --heal rewrites it to the single working key
  droid-switcher switch|sw|s [account]        Make an account active for normal droid runs (shares sessions by default)
  droid-switcher share-sessions|share|--share Remove organization locks so sessions are shared across accounts
  droid-switcher select|sel                   Pick a saved account interactively
  droid-switcher quota|limits|q [account]     Show Factory quota for a saved account
  droid-switcher quota|q --all|-a             Compare quota across every saved account
  droid-switcher menu|m                       Open the interactive menu
  droid-switcher list|ls                      List saved accounts
  droid-switcher current|cur                  Show the active account marker
  droid-switcher rename|mv <old> <new>        Rename a saved account
  droid-switcher label|lbl <account> <text>   Set a friendly label
  droid-switcher default|def <account>        Set the default account
  droid-switcher remove|rm <account>          Delete a saved account
  droid-switcher version|v                    Print the build version
  droid-switcher where|w                      Show switcher storage paths

Short flags:
  -a, --all      Show quota for all accounts
  --no-share     Keep organization isolation on switch (do not unlock sessions)
  -r, --raw      Include raw Factory limits response
  -n, --dry-run  Preview changes without modifying files
  -d, --droid    Path to the droid executable for login; accepted by quota for compatibility
  -l, --label    Friendly account label
  -f, --force    Overwrite existing saved account
  -c, --clear    Clear a label/default marker
  -y, --yes      Skip remove confirmation

The switcher stores account homes under ~/.droid-switcher/accounts and only
replaces Droid's auth files in ~/.factory when switching. Supported formats are
auth.v2.file/auth.v2.key (keyfile), auth.v2.keyring (Linux Secret Service), and
auth.v2.loginkeychain (macOS Login Keychain). Linux secure-storage accounts need
libsecret's secret-tool; macOS uses the built-in /usr/bin/security tool.

Use --raw with quota if Factory changes the limits response and you need the
original API output.`)
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
