package switcher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	factoryAPIBaseURLDefault   = "https://api.factory.ai"
	workOSBaseURLDefault       = "https://api.workos.com/user_management"
	factoryClientHeader        = "X-Factory-Client"
	factoryClientVersionHeader = "X-Client-Version"
	factoryOrgHeader           = "X-Factory-Org-Id"
	droidWorkOSClientIDDev     = "client_01HNM7927XNSKCJ4982Z5J3FFZ"
	droidWorkOSClientIDProd    = "client_01HNM792M5G5G1A2THWPXKFMXB"
	droidClientVersionDefault  = "0.187.0"
	tokenExpirySkew            = time.Minute
	tokenRefreshAttempts       = 3
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var (
	factoryAPIBaseURL          = factoryAPIBaseURLDefault
	workOSBaseURL              = workOSBaseURLDefault
	quotaHTTPClient   httpDoer = &http.Client{Timeout: 30 * time.Second}
)

type billingLimitsResponse struct {
	UsesTokenRateLimitsBilling bool                         `json:"usesTokenRateLimitsBilling"`
	Limits                     map[string]billingLimitGroup `json:"limits"`
	ExtraUsageAllowed          bool                         `json:"extraUsageAllowed"`
	ExtraUsageBalanceCents     int                          `json:"extraUsageBalanceCents"`
	OveragePreference          any                          `json:"overagePreference"`
}

type billingLimitGroup struct {
	FiveHour billingLimitWindow `json:"fiveHour"`
	Weekly   billingLimitWindow `json:"weekly"`
	Monthly  billingLimitWindow `json:"monthly"`
}

type billingLimitWindow struct {
	UsedPercent      float64 `json:"usedPercent"`
	SecondsRemaining int64   `json:"secondsRemaining"`
	WindowEnd        string  `json:"windowEnd"`
}

func fetchQuotaReport(ctx context.Context, p Paths, accountName string) (quotaReport, error) {
	name, err := CleanAccountName(accountName)
	if err != nil {
		return quotaReport{}, err
	}
	factoryHome := p.AccountFactoryHome(name)
	if err := EnsureSavedAuth(factoryHome); err != nil {
		return quotaReport{}, fmt.Errorf("saved account %q is not usable: %w", name, err)
	}
	creds, err := loadDroidCredentials(factoryHome)
	if err != nil {
		return quotaReport{}, fmt.Errorf("load saved Droid auth for %q: %w", name, err)
	}
	creds, refreshed, err := refreshDroidCredentialsIfNeeded(ctx, factoryHome, creds)
	if err != nil {
		var refreshErr tokenRefreshError
		if errors.As(err, &refreshErr) && refreshErr.code == "invalid_grant" {
			return quotaReport{}, fmt.Errorf(
				"saved Droid login for %q has ended; run droid-switcher login %s --force: %w",
				name,
				name,
				err,
			)
		}
		return quotaReport{}, err
	}
	if refreshed {
		if active, ok, err := CurrentAccount(p); err != nil {
			return quotaReport{}, err
		} else if ok && active == name {
			if err := copyDirFiles(factoryHome, p.FactoryHome, 0o600, authFilesForFormat(detectAuthFormat(factoryHome))); err != nil {
				return quotaReport{}, fmt.Errorf("sync refreshed active auth to current Factory home: %w", err)
			}
		}
	}
	raw, limits, err := getBillingLimits(ctx, creds)
	if err != nil {
		return quotaReport{}, err
	}
	meta, err := loadAccountMetadata(p, name)
	if err != nil {
		return quotaReport{}, err
	}
	return quotaReport{
		Account:                name,
		Label:                  meta.Label,
		Raw:                    prettyJSON(raw),
		Groups:                 limitGroupsFromResponse(limits),
		ExtraUsageAllowed:      limits.ExtraUsageAllowed,
		ExtraUsageBalanceCents: limits.ExtraUsageBalanceCents,
	}, nil
}

func refreshDroidCredentialsIfNeeded(ctx context.Context, factoryHome string, creds droidCredentials) (droidCredentials, bool, error) {
	expired, err := accessTokenExpired(creds.AccessToken, time.Now(), tokenExpirySkew)
	if err != nil {
		return droidCredentials{}, false, err
	}
	if !expired {
		return creds, false, nil
	}
	var lastErr error
	for attempt := 1; attempt <= tokenRefreshAttempts; attempt++ {
		next, err := refreshDroidCredentials(ctx, creds.RefreshToken)
		if err == nil {
			next.ActiveOrganizationID = creds.ActiveOrganizationID
			if err := saveDroidCredentials(factoryHome, next); err != nil {
				return droidCredentials{}, false, fmt.Errorf("save refreshed Droid auth: %w", err)
			}
			return next, true, nil
		}
		lastErr = err
		var refreshErr tokenRefreshError
		if errors.As(err, &refreshErr) && refreshErr.permanent {
			break
		}
		if attempt < tokenRefreshAttempts {
			select {
			case <-ctx.Done():
				return droidCredentials{}, false, ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
	}
	return droidCredentials{}, false, fmt.Errorf("refresh expired Droid login: %w", lastErr)
}

type tokenRefreshError struct {
	err       error
	permanent bool
	code      string
}

func (e tokenRefreshError) Error() string {
	return e.err.Error()
}

func (e tokenRefreshError) Unwrap() error {
	return e.err
}

func refreshDroidCredentials(ctx context.Context, refreshToken string) (droidCredentials, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", droidWorkOSClientID())
	// Droid's routine refresh deliberately omits organization_id; sending a
	// stale one makes WorkOS reject the refresh with organization_not_found.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(workOSBaseURL, "/authenticate"), strings.NewReader(form.Encode()))
	if err != nil {
		return droidCredentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return droidCredentials{}, tokenRefreshError{err: err}
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return droidCredentials{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var responseError struct {
			Code string `json:"error"`
		}
		_ = json.Unmarshal(body, &responseError)
		return droidCredentials{}, tokenRefreshError{
			err:       fmt.Errorf("WorkOS token refresh returned HTTP %d: %s", resp.StatusCode, compactSpace(string(body))),
			permanent: resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests,
			code:      responseError.Code,
		}
	}
	var next droidCredentials
	if err := json.Unmarshal(body, &next); err != nil {
		return droidCredentials{}, fmt.Errorf("parse WorkOS token refresh response: %w", err)
	}
	if strings.TrimSpace(next.AccessToken) == "" || strings.TrimSpace(next.RefreshToken) == "" {
		return droidCredentials{}, fmt.Errorf("WorkOS token refresh response did not include access_token and refresh_token")
	}
	return next, nil
}

func getBillingLimits(ctx context.Context, creds droidCredentials) ([]byte, billingLimitsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(factoryAPIBaseURL, "/api/billing/limits"), nil)
	if err != nil {
		return nil, billingLimitsResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(factoryClientHeader, factoryClientType())
	req.Header.Set(factoryClientVersionHeader, droidClientVersion())
	if creds.ActiveOrganizationID != "" {
		req.Header.Set(factoryOrgHeader, creds.ActiveOrganizationID)
	}
	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return nil, billingLimitsResponse{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, billingLimitsResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, billingLimitsResponse{}, fmt.Errorf("factory limits API returned HTTP %d: %s", resp.StatusCode, compactSpace(string(body)))
	}
	var limits billingLimitsResponse
	if err := json.Unmarshal(body, &limits); err != nil {
		return nil, billingLimitsResponse{}, fmt.Errorf("parse Factory limits response: %w", err)
	}
	return body, limits, nil
}

// limitGroupsFromResponse summarizes every billing pool the API reports
// (standard, core/Factory Core, and any future groups), standard first.
func limitGroupsFromResponse(resp billingLimitsResponse) []limitGroupReport {
	names := make([]string, 0, len(resp.Limits))
	for name := range resp.Limits {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "standard" {
			return true
		}
		if names[j] == "standard" {
			return false
		}
		return names[i] < names[j]
	})
	var groups []limitGroupReport
	for _, name := range names {
		group := resp.Limits[name]
		groups = append(groups, limitGroupReport{
			Name: name,
			Windows: []LimitWindow{
				{Window: "5h", Text: formatBillingWindow(group.FiveHour)},
				{Window: "1wk", Text: formatBillingWindow(group.Weekly)},
				{Window: "1month", Text: formatBillingWindow(group.Monthly)},
			},
		})
	}
	return groups
}

func formatBillingWindow(window billingLimitWindow) string {
	used := formatPercent(window.UsedPercent) + " used"
	if window.SecondsRemaining > 0 {
		return used + ", resets in " + formatDurationShort(time.Duration(window.SecondsRemaining)*time.Second)
	}
	if t, err := time.Parse(time.RFC3339, window.WindowEnd); err == nil && t.After(time.Now()) {
		return used + ", resets in " + formatDurationShort(time.Until(t))
	}
	// The API keeps reporting the last consumed window after it expires until
	// fresh usage opens a new one; do not present stale usage as current.
	return "idle (last window: " + used + ")"
}

func formatPercent(v float64) string {
	if math.Mod(v, 1) == 0 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

func formatDurationShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "<1m"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func accessTokenExpired(token string, now time.Time, skew time.Duration) (bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return true, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true, nil
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return true, nil
	}
	if claims.Exp == 0 {
		return true, nil
	}
	return !now.Add(skew).Before(time.Unix(claims.Exp, 0)), nil
}

func endpoint(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func droidWorkOSClientID() string {
	if strings.EqualFold(os.Getenv("FACTORY_ENV"), "development") {
		return droidWorkOSClientIDDev
	}
	return droidWorkOSClientIDProd
}

func factoryClientType() string {
	if v := strings.TrimSpace(os.Getenv("FACTORY_UPSTREAM_CLIENT_TYPE")); v != "" {
		return v
	}
	return "cli"
}

func droidClientVersion() string {
	if v := strings.TrimSpace(os.Getenv("DROID_SWITCHER_CLIENT_VERSION")); v != "" {
		return v
	}
	return droidClientVersionDefault
}
