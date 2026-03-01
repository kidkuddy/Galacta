package galacta

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port           int
	DataDir        string
	APIKey         string
	DefaultModel   string
	MaxConcurrency int
	MCPConfigPath  string
}

func LoadConfig() *Config {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = readKeychainAPIKey()
	}

	cfg := &Config{
		Port:           9090,
		DataDir:        defaultDataDir(),
		APIKey:         apiKey,
		DefaultModel:   "claude-sonnet-4-6",
		MaxConcurrency: 4,
		MCPConfigPath:  os.Getenv("GALACTA_MCP_CONFIG"),
	}

	if v := os.Getenv("GALACTA_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}

	if v := os.Getenv("GALACTA_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	if v := os.Getenv("GALACTA_DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}

	if v := os.Getenv("GALACTA_MAX_TOOL_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrency = n
		}
	}

	return cfg
}

// KeyFunc returns a function that reads the freshest API key available.
// The client caches the result and only calls this again on 401 to refresh.
// For static keys (env var) it returns the cached value; for OAuth (keychain)
// it re-reads so refreshed tokens are picked up.
func (c *Config) KeyFunc() func() string {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return func() string { return c.APIKey }
	}
	return func() string {
		if k := readKeychainAPIKey(); k != "" {
			return k
		}
		return c.APIKey
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".galacta"
	}
	return filepath.Join(home, ".galacta")
}

// readKeychainAPIKey attempts to read the Anthropic OAuth access token from
// macOS Keychain, where Claude Code stores it under "Claude Code-credentials".
func readKeychainAPIKey() string {
	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return ""
	}

	hexStr := strings.TrimSpace(string(out))
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}

	// The keychain blob has control byte prefixes from Electron's safeStorage
	// and may have trailing null bytes. The content is the inner fields of a
	// JSON object (missing outer braces).
	s := string(decoded)

	// Find where the JSON content starts (first `"`)
	start := strings.Index(s, "\"")
	if start < 0 {
		return ""
	}
	s = s[start:]

	// Trim trailing null bytes and junk after the last `}`
	if lastBrace := strings.LastIndex(s, "}"); lastBrace >= 0 {
		s = s[:lastBrace+1]
	}

	// Wrap in braces if it's not already a complete JSON object
	if !strings.HasPrefix(s, "{") {
		s = "{" + s + "}"
	}

	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(s), &creds); err != nil {
		return ""
	}

	if creds.ClaudeAiOauth.AccessToken != "" {
		log.Println("galacta: using API key from macOS Keychain (Claude Code credentials)")
		return creds.ClaudeAiOauth.AccessToken
	}

	return ""
}
