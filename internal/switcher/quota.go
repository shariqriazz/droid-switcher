package switcher

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// QuotaOptions selects quota targets and output detail.
type QuotaOptions struct {
	Account string
	All     bool
	Raw     bool
	Droid   string
	Stdin   io.Reader
}

// LimitWindow is one summarized Factory billing window.
type LimitWindow struct {
	Window string
	Text   string
}

// Quota reports limits for one account or every ready saved account.
func Quota(p Paths, opts QuotaOptions, stdout io.Writer) error {
	if err := SyncCurrentAuthToSavedAccount(p, io.Discard); err != nil {
		return err
	}
	if opts.All {
		if strings.TrimSpace(opts.Account) != "" {
			return fmt.Errorf("cannot combine --all with an explicit account")
		}
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
	Account                string
	Label                  string
	Raw                    string
	Groups                 []limitGroupReport
	ExtraUsageAllowed      bool
	ExtraUsageBalanceCents int
}

// limitGroupReport is one Factory billing pool (standard, core, ...) summarized
// into the 5h, 1wk, and 1month windows.
type limitGroupReport struct {
	Name    string
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
	hadErrors := false
	for i, account := range ready {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		report, err := quotaForAccount(p, account.Name, opts)
		if err != nil {
			hadErrors = true
			fmt.Fprintf(stdout, "%s\n  error: %v\n", account.Name, err)
			continue
		}
		printQuotaReport(stdout, report, opts.Raw)
	}
	if hadErrors {
		return fmt.Errorf("one or more accounts failed quota collection")
	}
	return nil
}

func quotaForAccount(p Paths, accountName string, opts QuotaOptions) (quotaReport, error) {
	_ = opts
	return fetchQuotaReport(context.Background(), p, accountName)
}

func printQuotaReport(stdout io.Writer, report quotaReport, raw bool) {
	if report.Label == "" {
		fmt.Fprintln(stdout, report.Account)
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", report.Label, report.Account)
	}
	if len(report.Groups) == 0 {
		if report.Raw == "" {
			fmt.Fprintln(stdout, "  no quota output returned")
			return
		}
		printIndented(stdout, report.Raw)
		return
	}
	for _, group := range report.Groups {
		fmt.Fprintf(stdout, "  %s\n", group.Name)
		for _, window := range group.Windows {
			fmt.Fprintf(stdout, "    %-6s %s\n", window.Window, window.Text)
		}
	}
	if report.ExtraUsageAllowed && report.ExtraUsageBalanceCents > 0 {
		fmt.Fprintf(stdout, "  extra usage balance: %s\n", formatCents(report.ExtraUsageBalanceCents))
	}
	if raw {
		fmt.Fprintln(stdout, "  raw:")
		printIndented(stdout, report.Raw)
	}
}

func formatCents(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

func printIndented(stdout io.Writer, text string) {
	for line := range strings.SplitSeq(text, "\n") {
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
