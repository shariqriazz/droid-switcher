package switcher

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// SessionSummary captures identity and organization constraint of a Droid session.
type SessionSummary struct {
	ID             string
	Title          string
	Path           string
	OrganizationID string
	CWD            string
}

// ShareOptions controls session unbinding and listing behavior.
type ShareOptions struct {
	SessionIDs []string
	DryRun     bool
	List       bool
	AdoptOrg   string
}

// FindSessions scans the Factory sessions directory for all session JSONL files.
func FindSessions(sessionsDir string, sessionIDs []string) ([]SessionSummary, error) {
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	var results []SessionSummary
	err := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible directories or entries gracefully.
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, ".") || strings.Contains(name, ".tmp-") {
			return nil
		}

		info, ok := readSessionSummary(path)
		if !ok {
			return nil
		}
		if len(sessionIDs) > 0 && !slices.Contains(sessionIDs, info.ID) {
			return nil
		}
		results = append(results, info)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ID < results[j].ID
	})
	return results, nil
}

func readSessionSummary(path string) (SessionSummary, bool) {
	file, err := os.Open(path) //nolint:gosec // Scoped by WalkDir within the sessions directory.
	if err != nil {
		return SessionSummary{}, false
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	firstLine, err := reader.ReadBytes('\n')
	if err != nil && len(firstLine) == 0 {
		return SessionSummary{}, false
	}

	var raw map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(firstLine), &raw); err != nil {
		return SessionSummary{}, false
	}

	typ, _ := raw["type"].(string)
	if typ != "session_start" {
		return SessionSummary{}, false
	}

	id, _ := raw["id"].(string)
	if id == "" {
		base := filepath.Base(path)
		id = strings.TrimSuffix(base, ".jsonl")
	}
	title, _ := raw["title"].(string)
	orgID, _ := raw["organizationId"].(string)
	cwd, _ := raw["cwd"].(string)

	return SessionSummary{
		ID:             id,
		Title:          title,
		Path:           path,
		OrganizationID: orgID,
		CWD:            cwd,
	}, true
}

// CountBoundSessions returns how many sessions in sessionsDir are bound to orgID (or any org if orgID is empty).
func CountBoundSessions(sessionsDir, orgID string) (int, error) {
	sessions, err := FindSessions(sessionsDir, nil)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, s := range sessions {
		if s.OrganizationID == "" {
			continue
		}
		if orgID == "" || s.OrganizationID == orgID {
			count++
		}
	}
	return count, nil
}

// MigrateStrandedSessions moves sessions created inside saved account homes (e.g. during login)
// into the live Factory sessions directory so Droid can see them.
func MigrateStrandedSessions(p Paths) (int, error) {
	if _, err := os.Stat(p.Accounts); os.IsNotExist(err) {
		return 0, nil
	}

	migrated := 0
	err := filepath.WalkDir(p.Accounts, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(p.Accounts, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 4 || parts[1] != factoryDirName || parts[2] != "sessions" {
			return nil
		}
		subPath := filepath.Join(parts[3:]...)
		dest := filepath.Join(p.FactorySessions(), subPath)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			if err := copyFile(path, dest, 0o600); err != nil {
				return err
			}
			migrated++
		}
		_ = os.Remove(path) //nolint:gosec // Scoped within switcher-owned account directories.
		return nil
	})
	return migrated, err
}

// ShareSessions removes organization constraints so sessions are accessible under any Droid account.
func ShareSessions(p Paths, opts ShareOptions, stdout io.Writer) error {
	_, _ = MigrateStrandedSessions(p)
	sessions, err := FindSessions(p.FactorySessions(), opts.SessionIDs)
	if err != nil {
		return fmt.Errorf("scan sessions: %w", err)
	}

	if opts.List {
		if len(sessions) == 0 {
			fmt.Fprintln(stdout, "No Droid sessions found.")
			return nil
		}
		fmt.Fprintf(stdout, "%-36s  %-20s  %-30s  %s\n", "SESSION ID", "ORGANIZATION", "TITLE", "CWD")
		for _, s := range sessions {
			orgDisplay := s.OrganizationID
			if orgDisplay == "" {
				orgDisplay = "<shared>"
			}
			title := s.Title
			if len(title) > 30 {
				title = title[:27] + "..."
			}
			fmt.Fprintf(stdout, "%-36s  %-20s  %-30s  %s\n", s.ID, orgDisplay, title, s.CWD)
		}
		return nil
	}

	var candidates []SessionSummary
	for _, s := range sessions {
		if opts.AdoptOrg != "" {
			if s.OrganizationID != opts.AdoptOrg {
				candidates = append(candidates, s)
			}
		} else {
			if s.OrganizationID != "" {
				candidates = append(candidates, s)
			}
		}
	}

	if len(candidates) == 0 {
		if len(sessions) == 0 {
			fmt.Fprintln(stdout, "No Droid sessions found.")
		} else {
			fmt.Fprintln(stdout, "All sessions are already shared across accounts.")
		}
		return nil
	}

	if opts.DryRun {
		for _, s := range candidates {
			displayTitle := s.Title
			if displayTitle == "" {
				displayTitle = "(no title)"
			}
			if opts.AdoptOrg != "" {
				fmt.Fprintf(stdout, "[dry-run] Would adopt %s %q: %s -> %s\n", s.ID, displayTitle, s.OrganizationID, opts.AdoptOrg)
			} else {
				fmt.Fprintf(stdout, "[dry-run] Would share %s %q: removing organization lock (%s)\n", s.ID, displayTitle, s.OrganizationID)
			}
		}
		fmt.Fprintf(stdout, "Dry run: %d session(s) would be updated.\n", len(candidates))
		return nil
	}

	updated := 0
	for _, s := range candidates {
		ok, err := modifySessionOrg(s.Path, opts.AdoptOrg)
		if err != nil {
			return fmt.Errorf("update session %s: %w", s.ID, err)
		}
		if ok {
			updated++
		}
	}

	if opts.AdoptOrg != "" {
		fmt.Fprintf(stdout, "Adopted %d session(s) into organization %s.\n", updated, opts.AdoptOrg)
	} else {
		fmt.Fprintf(stdout, "Shared %d session(s) across accounts (removed organization lock).\n", updated)
	}
	return nil
}

func modifySessionOrg(path, targetOrg string) (bool, error) {
	file, err := os.Open(path) //nolint:gosec // Scoped by internal session discovery.
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	firstLine, err := reader.ReadBytes('\n')
	if err != nil && len(firstLine) == 0 {
		return false, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(firstLine), &raw); err != nil {
		return false, nil
	}

	typ, _ := raw["type"].(string)
	if typ != "session_start" {
		return false, nil
	}

	currentOrg, hasOrg := raw["organizationId"].(string)
	if targetOrg == "" {
		if !hasOrg || currentOrg == "" {
			return false, nil // Already unbound.
		}
		delete(raw, "organizationId")
	} else {
		if hasOrg && currentOrg == targetOrg {
			return false, nil // Already set.
		}
		raw["organizationId"] = targetOrg
	}

	newFirstLine, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}

	tmpPath := path + ".tmp-" + randomSuffix()
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // tmp file in sessions dir.
	if err != nil {
		return false, err
	}

	writeErr := func() error {
		defer func() { _ = tmpFile.Close() }()
		if _, err := tmpFile.Write(append(newFirstLine, '\n')); err != nil {
			return err
		}
		if _, err := io.Copy(tmpFile, reader); err != nil {
			return err
		}
		return tmpFile.Sync()
	}()

	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return false, writeErr
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return false, err
	}

	return true, nil
}
