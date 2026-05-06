package switcher

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type QuotaOptions struct {
	Account string
	All     bool
	Raw     bool
	Droid   string
	Stdin   io.Reader
}

type LimitWindow struct {
	Window string
	Text   string
}

func Quota(p Paths, opts QuotaOptions, stdout io.Writer) error {
	if opts.All {
		return quotaAll(p, opts, stdout)
	}
	accountName := opts.Account
	if accountName == "" {
		var ok bool
		var err error
		accountName, ok, err = CurrentAccount(p)
		if err != nil {
			return err
		}
		if !ok {
			accountName, ok, err = CurrentDefaultAccount(p)
			if err != nil {
				return err
			}
			if !ok {
				accountName, err = SelectAccount(p, opts.Stdin, stdout)
				if err != nil {
					return fmt.Errorf("no active/default account: %w", err)
				}
			}
		}
	}
	report, err := quotaForAccount(p, accountName, opts)
	if err != nil {
		return err
	}
	printQuotaReport(stdout, report, opts.Raw)
	return nil
}

type quotaReport struct {
	Account string
	Label   string
	Raw     string
	Windows []LimitWindow
}

func quotaAll(p Paths, opts QuotaOptions, stdout io.Writer) error {
	accounts, err := ListAccounts(p)
	if err != nil {
		return err
	}
	var ready []AccountStatus
	for _, account := range accounts {
		if account.Ready {
			ready = append(ready, account)
		}
	}
	if len(ready) == 0 {
		return fmt.Errorf("no ready accounts saved; run droid-switcher login <account> first")
	}
	for i, account := range ready {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		report, err := quotaForAccount(p, account.Name, opts)
		if err != nil {
			fmt.Fprintf(stdout, "%s\n  error: %v\n", account.Name, err)
			continue
		}
		printQuotaReport(stdout, report, opts.Raw)
	}
	return nil
}

func quotaForAccount(p Paths, accountName string, opts QuotaOptions) (quotaReport, error) {
	name, err := CleanAccountName(accountName)
	if err != nil {
		return quotaReport{}, err
	}
	factoryHome := p.AccountFactoryHome(name)
	if err := EnsureAuth(factoryHome); err != nil {
		return quotaReport{}, fmt.Errorf("saved account %q is not usable: %w", name, err)
	}
	var out bytes.Buffer
	runner := NewDroidRunner(opts.Droid)
	runner.Stdout = &out
	if err := runDroid(runner, factoryHome, "exec", "--output-format", "text", "/limits"); err != nil {
		return quotaReport{}, err
	}
	raw := strings.TrimSpace(out.String())
	meta, err := loadAccountMetadata(p, name)
	if err != nil {
		return quotaReport{}, err
	}
	return quotaReport{
		Account: name,
		Label:   meta.Label,
		Raw:     raw,
		Windows: parseLimitWindows(raw),
	}, nil
}

func printQuotaReport(stdout io.Writer, report quotaReport, raw bool) {
	if report.Label == "" {
		fmt.Fprintln(stdout, report.Account)
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", report.Label, report.Account)
	}
	if len(report.Windows) == 0 {
		if report.Raw == "" {
			fmt.Fprintln(stdout, "  no quota output returned")
			return
		}
		printIndented(stdout, report.Raw)
		return
	}
	for _, window := range report.Windows {
		fmt.Fprintf(stdout, "  %-6s %s\n", window.Window, window.Text)
	}
	if raw {
		fmt.Fprintln(stdout, "  raw:")
		printIndented(stdout, report.Raw)
	}
}

func printIndented(stdout io.Writer, text string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		fmt.Fprintf(stdout, "  %s\n", line)
	}
}

func parseLimitWindows(raw string) []LimitWindow {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	windows := []string{"5h", "1wk", "1month"}
	var result []LimitWindow
	for _, window := range windows {
		if text := findWindowText(lines, window); text != "" {
			result = append(result, LimitWindow{Window: window, Text: text})
		}
	}
	return result
}

func findWindowText(lines []string, window string) string {
	pattern := regexp.MustCompile(`(?i)(^|[^[:alnum:]])` + regexp.QuoteMeta(window) + `([^[:alnum:]]|$)`)
	for i, line := range lines {
		clean := compactSpace(stripMarkdown(line))
		if !pattern.MatchString(clean) {
			continue
		}
		if value := strings.Trim(clean, " -:\t"); value != "" {
			return value
		}
		if i+1 < len(lines) {
			return compactSpace(stripMarkdown(lines[i+1]))
		}
	}
	return ""
}

func stripMarkdown(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "-*| ")
	line = strings.TrimRight(line, "| ")
	line = strings.ReplaceAll(line, "`", "")
	return line
}

func compactSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
