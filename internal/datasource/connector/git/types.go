// Package gitconnector implements a generic Git data source connector.
package gitconnector

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const defaultMaxFileBytes int64 = 50 * 1024 * 1024

var (
	scpRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@([A-Za-z0-9.-]+):(.+)$`)
	versionParts   = regexp.MustCompile(`\d+|\D+`)
)

var defaultExtensions = []string{
	".csv", ".doc", ".docx", ".html", ".htm", ".md", ".markdown",
	".pdf", ".ppt", ".pptx", ".txt", ".xls", ".xlsx",
}

type Config struct {
	RepoURL           string
	Branch            string
	Path              string
	LatestParent      string
	LatestPattern     string
	IncludeExtensions map[string]struct{}
	MaxFileBytes      int64
	Username          string
	Token             string
	PrivateKey        string
	KnownHosts        string
}

type gitCursor struct {
	Commit       string            `json:"commit"`
	SelectedRoot string            `json:"selected_root,omitempty"`
	Files        map[string]string `json:"files"`
}

func parseConfig(config *types.DataSourceConfig) (*Config, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: config is nil", datasource.ErrInvalidConfig)
	}

	settings := config.Settings
	credentials := config.Credentials
	cfg := &Config{
		RepoURL:       firstString(settings, credentials, "repo_url"),
		Branch:        firstString(settings, credentials, "branch"),
		Path:          firstString(settings, credentials, "path"),
		LatestParent:  firstString(settings, credentials, "latest_parent"),
		LatestPattern: firstString(settings, credentials, "latest_pattern"),
		Username:      stringValue(credentials, "username"),
		Token:         firstString(credentials, credentials, "token", "password"),
		PrivateKey:    stringValue(credentials, "private_key"),
		KnownHosts:    stringValue(credentials, "known_hosts"),
		MaxFileBytes:  int64Value(settings, "max_file_bytes", defaultMaxFileBytes),
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	cfg.IncludeExtensions = extensionSet(stringListValue(settings, "include_extensions"))
	if len(cfg.IncludeExtensions) == 0 {
		cfg.IncludeExtensions = extensionSet(defaultExtensions)
	}

	if err := validateRepoURL(cfg.RepoURL); err != nil {
		return nil, err
	}
	if err := validateRef(cfg.Branch); err != nil {
		return nil, err
	}
	for label, value := range map[string]string{
		"path":          cfg.Path,
		"latest_parent": cfg.LatestParent,
	} {
		if value != "" {
			if _, err := normalizeRepoPath(value); err != nil {
				return nil, fmt.Errorf("%s: %w", label, err)
			}
		}
	}
	if (cfg.LatestParent == "") != (cfg.LatestPattern == "") {
		return nil, fmt.Errorf("%w: latest_parent and latest_pattern must be configured together", datasource.ErrInvalidConfig)
	}
	if strings.Contains(cfg.LatestPattern, "/") || cfg.LatestPattern == "." || cfg.LatestPattern == ".." {
		return nil, fmt.Errorf("%w: latest_pattern must match one directory name", datasource.ErrInvalidConfig)
	}
	if cfg.LatestPattern != "" {
		if _, err := path.Match(cfg.LatestPattern, "version-placeholder"); err != nil {
			return nil, fmt.Errorf("%w: invalid latest_pattern: %v", datasource.ErrInvalidConfig, err)
		}
	}
	if cfg.MaxFileBytes <= 0 || cfg.MaxFileBytes > 100*1024*1024 {
		return nil, fmt.Errorf("%w: max_file_bytes must be between 1 and 104857600", datasource.ErrInvalidConfig)
	}
	if isSSHRepo(cfg.RepoURL) && strings.TrimSpace(cfg.KnownHosts) == "" {
		return nil, fmt.Errorf("%w: known_hosts is required for SSH repositories", datasource.ErrInvalidCredentials)
	}
	return cfg, nil
}

func firstString(primary, fallback map[string]interface{}, keys ...string) string {
	for _, values := range []map[string]interface{}{primary, fallback} {
		for _, key := range keys {
			if value := stringValue(values, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func stringValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func int64Value(values map[string]interface{}, key string, fallback int64) int64 {
	if len(values) == 0 {
		return fallback
	}
	switch value := values[key].(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func stringListValue(values map[string]interface{}, key string) []string {
	if len(values) == 0 {
		return nil
	}
	switch value := values[key].(type) {
	case string:
		return strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		})
	case []string:
		return value
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func extensionSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		out[value] = struct{}{}
	}
	return out
}

func validateRepoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: repo_url is required", datasource.ErrInvalidConfig)
	}
	if strings.HasPrefix(raw, "-") {
		return fmt.Errorf("%w: invalid repo_url", datasource.ErrInvalidConfig)
	}

	host := ""
	switch {
	case strings.HasPrefix(raw, "https://"):
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%w: invalid HTTPS repo_url", datasource.ErrInvalidConfig)
		}
		host = parsed.Hostname()
	case strings.HasPrefix(raw, "ssh://"):
		parsed, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("%w: invalid SSH repo_url", datasource.ErrInvalidConfig)
		}
		hasPassword := false
		if parsed.User != nil {
			_, hasPassword = parsed.User.Password()
		}
		if parsed.Hostname() == "" || hasPassword ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%w: invalid SSH repo_url", datasource.ErrInvalidConfig)
		}
		host = parsed.Hostname()
	default:
		match := scpRepoPattern.FindStringSubmatch(raw)
		if len(match) != 3 || strings.TrimSpace(match[2]) == "" {
			return fmt.Errorf("%w: repo_url must use HTTPS, SSH, or SCP syntax", datasource.ErrInvalidConfig)
		}
		host = match[1]
	}
	if err := secutils.ValidateURLForSSRF("https://" + host); err != nil {
		// Transparent proxies (for example Clash fake-IP mode) resolve arbitrary
		// public domains into RFC 2544's 198.18.0.0/15 range. Git still connects
		// by hostname through that proxy, so requiring every repository domain to
		// be allowlisted would make the connector unusable. Only relax this exact
		// proxy-resolution case; localhost, private networks, metadata hosts and
		// direct IP repository URLs continue to fail the central SSRF check.
		if isProxyFakeIPValidationError(host, err) {
			return nil
		}
		return fmt.Errorf("repo_url SSRF validation failed: %w", err)
	}
	return nil
}

func isProxyFakeIPValidationError(host string, err error) bool {
	return err != nil && net.ParseIP(host) == nil &&
		strings.Contains(err.Error(), "restricted range 198.18.0.0/15")
}

func validateRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, " \t\r\n~^:?*[\\") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".lock") {
		return fmt.Errorf("%w: invalid branch", datasource.ErrInvalidConfig)
	}
	return nil
}

func normalizeRepoPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." {
		return ".", nil
	}
	cleaned := path.Clean(value)
	if strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: repository path must not escape the repository", datasource.ErrInvalidConfig)
	}
	return strings.TrimPrefix(cleaned, "./"), nil
}

func isSSHRepo(repoURL string) bool {
	return strings.HasPrefix(repoURL, "ssh://") || scpRepoPattern.MatchString(repoURL)
}

func decodeCursor(cursor *types.SyncCursor) *gitCursor {
	if cursor == nil || cursor.ConnectorCursor == nil {
		return nil
	}
	data, err := json.Marshal(cursor.ConnectorCursor)
	if err != nil {
		return nil
	}
	var decoded gitCursor
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Files == nil {
		return nil
	}
	return &decoded
}

func encodeCursor(cursor gitCursor) *types.SyncCursor {
	data, _ := json.Marshal(cursor)
	var connectorCursor map[string]interface{}
	_ = json.Unmarshal(data, &connectorCursor)
	return &types.SyncCursor{
		ConnectorCursor: connectorCursor,
		LastSchemaHash:  cursor.Commit,
	}
}

func sortStrings(values []string) {
	sort.Strings(values)
}

func naturalLess(left, right string) bool {
	leftParts := versionParts.FindAllString(left, -1)
	rightParts := versionParts.FindAllString(right, -1)
	for i := 0; i < len(leftParts) && i < len(rightParts); i++ {
		l, r := leftParts[i], rightParts[i]
		ln, lerr := strconv.Atoi(l)
		rn, rerr := strconv.Atoi(r)
		if lerr == nil && rerr == nil {
			if ln != rn {
				return ln < rn
			}
			continue
		}
		if l != r {
			return l < r
		}
	}
	return len(leftParts) < len(rightParts)
}
