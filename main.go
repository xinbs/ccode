package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	appName          = "ccode"
	appID            = "ccode-openrouter"
	defaultBaseURL   = "https://openrouter.ai/api"
	defaultReferer   = "https://localhost"
	defaultTitle     = "ccode-openrouter"
	defaultConfigDir = "ccode-openrouter"
)

var (
	version                  = "0.1.0-dev"
	commit                   = "unknown"
	buildTime                = "unknown"
	openRouterRankingPattern = regexp.MustCompile(`href="/([^/?"#]+/[^/?"#]+)"`)
	managedEnvVars           = []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
		"LLM_MODEL",
		"LLM_PROVIDER",
	}
)

type config struct {
	OpenRouterAPIKey    string `json:"openrouter_api_key"`
	OpenRouterAPIKeyEnv string `json:"openrouter_api_key_env"`
	BaseURL             string `json:"base_url"`
	LaunchCmd           string `json:"launch_cmd"`
	DefaultModel        string `json:"default_model"`
	HTTPReferer         string `json:"http_referer"`
	Title               string `json:"title"`
}

type storedSecret struct {
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type runtimeOptions struct {
	Model       string
	Top         int
	NoRankings  bool
	ClaudeArgs  []string
	CommandMode string
	JSON        bool
}

type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string                      `json:"id"`
	CanonicalSlug string                      `json:"canonical_slug"`
	Name          string                      `json:"name"`
	Created       int64                       `json:"created"`
	ContextLength int                         `json:"context_length"`
	Architecture  openRouterModelArchitecture `json:"architecture"`
	Pricing       openRouterModelPricing      `json:"pricing"`
	TopProvider   openRouterTopProvider       `json:"top_provider"`
	Supported     []string                    `json:"supported_parameters"`
	ExpiresAt     any                         `json:"expiration_date"`
}

type openRouterModelArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type openRouterModelPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type openRouterTopProvider struct {
	ContextLength int `json:"context_length"`
}

type openRouterModelOption struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	CanonicalSlug     string    `json:"canonical_slug,omitempty"`
	ContextLength     int       `json:"context_length,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	IsHot             bool      `json:"is_hot,omitempty"`
	IsNew             bool      `json:"is_new,omitempty"`
	SupportsTools     bool      `json:"supports_tools,omitempty"`
	SupportsReasoning bool      `json:"supports_reasoning,omitempty"`
	RankingIndex      int       `json:"-"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	loadEnvFiles()

	if len(args) == 0 {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return handleLaunch(cfg, runtimeOptions{CommandMode: "launch", Top: 20})
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage()
		return nil
	case "version":
		return handleVersion()
	case "key":
		return handleKey(args[1:])
	case "models":
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		opts, err := parseOptions(args[1:], "models")
		if err != nil {
			return err
		}
		return handleModels(cfg, opts)
	case "env":
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		opts, err := parseOptions(args[1:], "env")
		if err != nil {
			return err
		}
		return handleEnv(cfg, opts)
	case "launch":
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		opts, err := parseOptions(args[1:], "launch")
		if err != nil {
			return err
		}
		return handleLaunch(cfg, opts)
	case "unset":
		handleUnset()
		return nil
	default:
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		opts, err := parseOptions(args, "launch")
		if err != nil {
			return err
		}
		return handleLaunch(cfg, opts)
	}
}

func printUsage() {
	fmt.Println(strings.TrimSpace(`
Usage:
  ccode
  ccode launch [--model <model-id>] [--top <n>] [--no-rankings] [--] [claude args...]
  ccode env [--model <model-id>] [--top <n>] [--no-rankings]
  ccode models [--top <n>] [--json] [--no-rankings]
  ccode key clear
  ccode unset
  ccode version

Notes:
  - This open-source edition only supports OpenRouter free models.
  - It fetches the current free model list from OpenRouter and launches Claude Code.
  - If Claude Code is missing, ccode will try to install it automatically using Anthropic's official installer.
  - If no key is configured, interactive mode will prompt for it and can save an encrypted local copy.
`))
}

func handleVersion() error {
	info := map[string]string{
		"app_id":     appID,
		"version":    version,
		"commit":     commit,
		"build_time": buildTime,
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
	}
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func handleKey(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ccode key clear")
	}
	switch args[0] {
	case "clear":
		err := os.Remove(secretFilePath())
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("cleared saved key: %s\n", secretFilePath())
		return nil
	default:
		return fmt.Errorf("unknown key command: %s", args[0])
	}
}

func parseOptions(args []string, mode string) (runtimeOptions, error) {
	opts := runtimeOptions{
		CommandMode: mode,
		Top:         20,
	}
	if mode == "models" {
		opts.Top = 50
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			i++
			if i >= len(args) {
				return opts, errors.New("missing value for --model")
			}
			opts.Model = strings.TrimSpace(args[i])
		case "--top":
			i++
			if i >= len(args) {
				return opts, errors.New("missing value for --top")
			}
			value, err := strconv.Atoi(args[i])
			if err != nil || value <= 0 {
				return opts, errors.New("invalid value for --top")
			}
			opts.Top = value
		case "--no-rankings":
			opts.NoRankings = true
		case "--json":
			if mode != "models" {
				return opts, errors.New("--json is only supported by `models`")
			}
			opts.JSON = true
		case "--":
			if mode != "launch" {
				return opts, errors.New("`--` is only supported by `launch`")
			}
			opts.ClaudeArgs = append(opts.ClaudeArgs, args[i+1:]...)
			return opts, nil
		default:
			return opts, fmt.Errorf("unknown argument: %s", args[i])
		}
	}
	return opts, nil
}

func handleModels(cfg config, opts runtimeOptions) error {
	apiKey, err := resolveAPIKey(cfg)
	if err != nil {
		return err
	}
	options, err := fetchModelOptions(cfg, apiKey, opts.NoRankings)
	if err != nil {
		return err
	}
	if opts.Top > 0 && len(options) > opts.Top {
		options = options[:opts.Top]
	}
	if opts.JSON {
		raw, err := json.MarshalIndent(options, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	for idx, option := range options {
		fmt.Printf("%d. %s\n", idx+1, formatOpenRouterModelOption(option))
	}
	return nil
}

func handleEnv(cfg config, opts runtimeOptions) error {
	apiKey, err := resolveAPIKey(cfg)
	if err != nil {
		return err
	}
	model, err := resolveSelectedModel(cfg, apiKey, opts)
	if err != nil {
		return err
	}
	fmt.Print(shellExportLines(buildClaudeEnv(cfg, apiKey, model)))
	return nil
}

func handleLaunch(cfg config, opts runtimeOptions) error {
	apiKey, err := resolveAPIKey(cfg)
	if err != nil {
		return err
	}
	model, err := resolveSelectedModel(cfg, apiKey, opts)
	if err != nil {
		return err
	}
	return launchClaude(cfg, buildClaudeEnv(cfg, apiKey, model), opts.ClaudeArgs)
}

func resolveSelectedModel(cfg config, apiKey string, opts runtimeOptions) (string, error) {
	if strings.TrimSpace(opts.Model) != "" {
		return strings.TrimSpace(opts.Model), nil
	}

	options, err := fetchModelOptions(cfg, apiKey, opts.NoRankings)
	if err != nil {
		if strings.TrimSpace(cfg.DefaultModel) != "" {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch model list, falling back to default_model=%s\n", cfg.DefaultModel)
			return strings.TrimSpace(cfg.DefaultModel), nil
		}
		return "", err
	}
	if len(options) == 0 {
		if strings.TrimSpace(cfg.DefaultModel) != "" {
			fmt.Fprintf(os.Stderr, "warning: no free model found, falling back to default_model=%s\n", cfg.DefaultModel)
			return strings.TrimSpace(cfg.DefaultModel), nil
		}
		return "", errors.New("no OpenRouter free text models available")
	}

	if !isInteractiveTerminal() {
		fmt.Fprintf(os.Stderr, "selected model: %s\n", options[0].ID)
		return options[0].ID, nil
	}
	return chooseOpenRouterModel(options, opts.Top)
}

func fetchModelOptions(cfg config, apiKey string, noRankings bool) ([]openRouterModelOption, error) {
	models, err := fetchOpenRouterModels(cfg, apiKey)
	if err != nil {
		return nil, err
	}

	var rankings []string
	if !noRankings {
		rankings, err = fetchOpenRouterRankings(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to read rankings, using local sort only: %v\n", err)
		}
	}

	options := buildOpenRouterModelOptions(models, rankings, time.Now())
	if len(options) == 0 {
		return nil, errors.New("OpenRouter returned no free text models")
	}
	return options, nil
}

func resolveAPIKey(cfg config) (string, error) {
	if value := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("CCODE_OPENROUTER_API_KEY")); value != "" {
		return value, nil
	}
	if envName := strings.TrimSpace(cfg.OpenRouterAPIKeyEnv); envName != "" {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value, nil
		}
	}
	if value, err := loadSavedAPIKey(); err == nil && strings.TrimSpace(value) != "" {
		return value, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: failed to read saved key, ignoring it: %v\n", err)
	}
	if value := strings.TrimSpace(cfg.OpenRouterAPIKey); value != "" {
		return value, nil
	}
	if !isInteractiveTerminal() {
		return "", errors.New("missing OpenRouter API key; set OPENROUTER_API_KEY, config.openrouter_api_key_env, or run `ccode` interactively once to save it")
	}

	value, err := promptDefault("OpenRouter API key", "")
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("OpenRouter API key cannot be empty")
	}

	saveKey, err := promptYesNo("Save the key locally in encrypted form", true)
	if err != nil {
		return "", err
	}
	if saveKey {
		if err := saveAPIKey(value); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save encrypted key: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "saved encrypted key to %s\n", secretFilePath())
		}
	}
	return value, nil
}

func buildClaudeEnv(cfg config, apiKey, model string) map[string]string {
	baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("CCODE_BASE_URL"), cfg.BaseURL, defaultBaseURL), "/")
	return map[string]string{
		"ANTHROPIC_BASE_URL":             baseURL,
		"ANTHROPIC_AUTH_TOKEN":           apiKey,
		"ANTHROPIC_API_KEY":              "",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  model,
		"ANTHROPIC_MODEL":                model,
		"CLAUDE_CODE_SUBAGENT_MODEL":     model,
		"LLM_MODEL":                      model,
		"LLM_PROVIDER":                   "openrouter",
	}
}

func launchClaude(cfg config, envs map[string]string, claudeArgs []string) error {
	command, configured := resolveLaunchCommand(cfg)
	if len(command) == 0 {
		if shouldAutoInstallClaude(configured) {
			fmt.Fprintln(os.Stderr, "Claude Code not found. Installing it now...")
			if err := installClaudeCode(); err != nil {
				return err
			}
			command, configured = resolveLaunchCommand(cfg)
		}
		if len(command) == 0 {
			return fmt.Errorf("cannot find Claude Code executable after install attempt; checked launch_cmd=%q", configured)
		}
	}

	cmd := exec.Command(command[0], append(command[1:], claudeArgs...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}
	for _, key := range managedEnvVars {
		delete(envMap, key)
	}
	for key, value := range envs {
		envMap[key] = value
	}
	cmd.Env = flattenEnvMap(envMap)

	fmt.Fprintf(os.Stderr, "launching %s\n", strings.Join(append(command, claudeArgs...), " "))
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func shouldAutoInstallClaude(configured string) bool {
	parts, err := shellSplit(strings.TrimSpace(configured))
	if err != nil || len(parts) == 0 {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(filepath.Base(parts[0])))
	switch name {
	case "claude", "claude.cmd", "claude.exe":
		return true
	default:
		return false
	}
}

func installClaudeCode() error {
	if runtime.GOOS == "windows" {
		return installClaudeCodeWindows()
	}
	return installClaudeCodeUnix()
}

func installClaudeCodeUnix() error {
	if _, err := exec.LookPath("bash"); err != nil {
		return errors.New("bash is required to run the Claude installer")
	}

	var script string
	switch {
	case commandExists("curl"):
		script = "curl -fsSL https://claude.ai/install.sh | bash"
	case commandExists("wget"):
		script = "wget -qO- https://claude.ai/install.sh | bash"
	default:
		return errors.New("curl or wget is required to install Claude Code automatically")
	}

	cmd := exec.Command("bash", "-lc", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Claude Code install failed: %w", err)
	}
	return nil
}

func installClaudeCodeWindows() error {
	shell := ""
	switch {
	case commandExists("powershell"):
		shell = "powershell"
	case commandExists("pwsh"):
		shell = "pwsh"
	default:
		return errors.New("PowerShell is required to install Claude Code automatically on Windows")
	}

	cmd := exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://claude.ai/install.ps1 | iex")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Claude Code install failed: %w", err)
	}
	return nil
}

func resolveLaunchCommand(cfg config) ([]string, string) {
	configured := strings.TrimSpace(firstNonEmpty(os.Getenv("CCODE_LAUNCH_CMD"), cfg.LaunchCmd, "claude"))
	if configured == "" {
		configured = "claude"
	}
	parts, err := shellSplit(configured)
	if err != nil || len(parts) == 0 {
		parts = []string{"claude"}
	}

	executable := parts[0]
	if strings.ContainsRune(executable, os.PathSeparator) {
		if isExecutable(executable) {
			return parts, configured
		}
		return nil, configured
	}
	if found, err := exec.LookPath(executable); err == nil {
		parts[0] = found
		return parts, configured
	}
	if executable == "claude" {
		for _, candidate := range claudeSearchPaths() {
			if isExecutable(candidate) {
				parts[0] = candidate
				return parts, configured
			}
		}
	}
	return nil, configured
}

func fetchOpenRouterModels(cfg config, apiKey string) ([]openRouterModel, error) {
	var payload openRouterModelsResponse
	if err := doOpenRouterJSONRequest(cfg, http.MethodGet, "https://openrouter.ai/api/v1/models", apiKey, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func fetchOpenRouterRankings(cfg config) ([]string, error) {
	body, err := doOpenRouterTextRequest(cfg, http.MethodGet, "https://openrouter.ai/rankings", "")
	if err != nil {
		return nil, err
	}
	rankings := parseOpenRouterRankingSlugs(string(body))
	if len(rankings) == 0 {
		return nil, errors.New("no ranking slugs parsed from OpenRouter rankings page")
	}
	return rankings, nil
}

func doOpenRouterJSONRequest(cfg config, method, requestURL, apiKey string, out any) error {
	body, err := doOpenRouterTextRequest(cfg, method, requestURL, apiKey)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode OpenRouter response: %w", err)
	}
	return nil
}

func doOpenRouterTextRequest(cfg config, method, requestURL, apiKey string) ([]byte, error) {
	req, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", appID+"/"+version)
	req.Header.Set("X-Title", firstNonEmpty(cfg.Title, defaultTitle))
	req.Header.Set("HTTP-Referer", firstNonEmpty(cfg.HTTPReferer, defaultReferer))
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenRouter request failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func buildOpenRouterModelOptions(models []openRouterModel, rankings []string, now time.Time) []openRouterModelOption {
	rankingIndex := map[string]int{}
	for idx, slug := range rankings {
		normalized := strings.TrimSpace(slug)
		if normalized == "" {
			continue
		}
		if _, exists := rankingIndex[normalized]; !exists {
			rankingIndex[normalized] = idx
		}
	}

	options := make([]openRouterModelOption, 0, len(models))
	for _, model := range models {
		if !shouldIncludeOpenRouterModel(model, now) {
			continue
		}

		index := 1 << 30
		if value, ok := rankingIndex[strings.TrimSpace(model.ID)]; ok {
			index = value
		} else if value, ok := rankingIndex[strings.TrimSpace(model.CanonicalSlug)]; ok {
			index = value
		}

		contextLength := model.ContextLength
		if model.TopProvider.ContextLength > contextLength {
			contextLength = model.TopProvider.ContextLength
		}

		options = append(options, openRouterModelOption{
			ID:                strings.TrimSpace(model.ID),
			Name:              strings.TrimSpace(model.Name),
			CanonicalSlug:     strings.TrimSpace(model.CanonicalSlug),
			ContextLength:     contextLength,
			CreatedAt:         time.Unix(model.Created, 0),
			IsHot:             index != 1<<30,
			IsNew:             isRecentUnixTimestamp(model.Created, now, 45*24*time.Hour),
			SupportsTools:     openRouterSupportsParameter(model.Supported, "tools") || openRouterSupportsParameter(model.Supported, "tool_choice"),
			SupportsReasoning: openRouterSupportsParameter(model.Supported, "reasoning") || openRouterSupportsParameter(model.Supported, "include_reasoning"),
			RankingIndex:      index,
		})
	}

	sort.SliceStable(options, func(i, j int) bool {
		left, right := options[i], options[j]
		switch {
		case left.IsHot != right.IsHot:
			return left.IsHot
		case left.RankingIndex != right.RankingIndex:
			return left.RankingIndex < right.RankingIndex
		case left.IsNew != right.IsNew:
			return left.IsNew
		}

		leftQuality := openRouterQualityScore(left)
		rightQuality := openRouterQualityScore(right)
		switch {
		case leftQuality != rightQuality:
			return leftQuality > rightQuality
		case !left.CreatedAt.Equal(right.CreatedAt):
			return left.CreatedAt.After(right.CreatedAt)
		default:
			return left.ID < right.ID
		}
	})
	return options
}

func shouldIncludeOpenRouterModel(model openRouterModel, now time.Time) bool {
	id := strings.TrimSpace(model.ID)
	if id == "" || strings.HasPrefix(id, "openrouter/") {
		return false
	}
	if !openRouterModelIsFree(model) {
		return false
	}
	if !openRouterModelSupportsTextIO(model) {
		return false
	}
	if openRouterModelExpired(model, now) {
		return false
	}
	return true
}

func openRouterModelIsFree(model openRouterModel) bool {
	if strings.HasSuffix(strings.TrimSpace(model.ID), ":free") {
		return true
	}
	return openRouterPriceIsZero(model.Pricing.Prompt) && openRouterPriceIsZero(model.Pricing.Completion)
}

func openRouterPriceIsZero(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return false
	}
	return parsed == 0
}

func openRouterModelSupportsTextIO(model openRouterModel) bool {
	inputs := normalizeStringList(model.Architecture.InputModalities)
	outputs := normalizeStringList(model.Architecture.OutputModalities)
	if len(inputs) > 0 && len(outputs) > 0 {
		return containsString(inputs, "text") && containsString(outputs, "text")
	}

	modality := strings.ToLower(strings.TrimSpace(model.Architecture.Modality))
	if modality == "" {
		return true
	}
	parts := strings.SplitN(modality, "->", 2)
	if len(parts) != 2 {
		return strings.Contains(modality, "text")
	}
	return strings.Contains(parts[0], "text") && strings.Contains(parts[1], "text")
}

func openRouterModelExpired(model openRouterModel, now time.Time) bool {
	switch typed := model.ExpiresAt.(type) {
	case string:
		expiresAt, ok := parseOpenRouterTime(typed)
		return ok && now.After(expiresAt)
	default:
		return false
	}
}

func parseOpenRouterTime(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isRecentUnixTimestamp(ts int64, now time.Time, maxAge time.Duration) bool {
	if ts <= 0 {
		return false
	}
	return now.Sub(time.Unix(ts, 0)) <= maxAge
}

func openRouterSupportsParameter(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func openRouterQualityScore(option openRouterModelOption) int {
	score := 0
	if option.SupportsTools {
		score += 4
	}
	if option.SupportsReasoning {
		score += 3
	}
	switch {
	case option.ContextLength >= 1_000_000:
		score += 3
	case option.ContextLength >= 200_000:
		score += 2
	case option.ContextLength >= 64_000:
		score++
	}
	return score
}

func parseOpenRouterRankingSlugs(html string) []string {
	matches := openRouterRankingPattern.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil
	}

	blocked := map[string]struct{}{
		"_next":                 {},
		"about":                 {},
		"announcements":         {},
		"apps":                  {},
		"careers":               {},
		"chat":                  {},
		"data":                  {},
		"docs":                  {},
		"enterprise":            {},
		"labs":                  {},
		"models":                {},
		"pricing":               {},
		"privacy":               {},
		"providers":             {},
		"rankings":              {},
		"state-of-ai":           {},
		"support":               {},
		"terms":                 {},
		"works-with-openrouter": {},
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		slug := strings.TrimSpace(match[1])
		parts := strings.SplitN(slug, "/", 2)
		if len(parts) != 2 {
			continue
		}
		if _, blockedSlug := blocked[parts[0]]; blockedSlug {
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}

func chooseOpenRouterModel(options []openRouterModelOption, top int) (string, error) {
	filtered := append([]openRouterModelOption(nil), options...)
	if top <= 0 {
		top = 20
	}

	for {
		displayed := filtered
		if len(displayed) > top {
			displayed = displayed[:top]
		}

		fmt.Println("OpenRouter free models:")
		for idx, option := range displayed {
			fmt.Printf("%d. %s\n", idx+1, formatOpenRouterModelOption(option))
		}

		choice, err := promptDefault("Select a model number, type a prefix to filter, or `/` to reset", "1")
		if err != nil {
			return "", err
		}
		choice = strings.TrimSpace(choice)
		if choice == "/" {
			filtered = append([]openRouterModelOption(nil), options...)
			continue
		}
		if isDigits(choice) {
			index, _ := strconv.Atoi(choice)
			index--
			if index < 0 || index >= len(displayed) {
				fmt.Println("Invalid selection.")
				continue
			}
			return displayed[index].ID, nil
		}

		next := filterOpenRouterModelOptions(options, choice)
		if len(next) == 0 {
			fmt.Println("No model matches that prefix.")
			continue
		}
		filtered = next
	}
}

func filterOpenRouterModelOptions(options []openRouterModelOption, query string) []openRouterModelOption {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]openRouterModelOption(nil), options...)
	}
	filtered := make([]openRouterModelOption, 0, len(options))
	for _, option := range options {
		for _, candidate := range []string{option.ID, option.Name, option.CanonicalSlug} {
			if openRouterHasPrefix(candidate, query) {
				filtered = append(filtered, option)
				break
			}
		}
	}
	return filtered
}

func openRouterHasPrefix(value, query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || query == "" {
		return false
	}
	if strings.HasPrefix(normalized, query) {
		return true
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '/', '-', '_', ' ', '(', ')', ':':
			return true
		default:
			return false
		}
	})
	for _, part := range parts {
		if strings.HasPrefix(part, query) {
			return true
		}
	}
	return false
}

func formatOpenRouterModelOption(option openRouterModelOption) string {
	parts := []string{option.ID}
	if option.Name != "" && option.Name != option.ID {
		parts = append(parts, "("+option.Name+")")
	}

	tags := make([]string, 0, 4)
	if option.IsHot {
		tags = append(tags, "hot")
	}
	if option.IsNew {
		tags = append(tags, "new")
	}
	if option.SupportsTools {
		tags = append(tags, "tools")
	}
	if option.SupportsReasoning {
		tags = append(tags, "reasoning")
	}
	if len(tags) > 0 {
		parts = append(parts, "["+strings.Join(tags, ",")+"]")
	}
	if option.ContextLength > 0 {
		parts = append(parts, "ctx="+formatContextLength(option.ContextLength))
	}
	return strings.Join(parts, " ")
}

func formatContextLength(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%dM", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%dK", value/1_000)
	default:
		return strconv.Itoa(value)
	}
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func containsString(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func loadConfig() (config, error) {
	path := firstExistingFile(configCandidates())
	if path == "" {
		return config{}, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return config{}, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	return cfg, nil
}

func loadSavedAPIKey() (string, error) {
	raw, err := os.ReadFile(secretFilePath())
	if err != nil {
		return "", err
	}

	var secret storedSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return "", err
	}
	if secret.Version != 1 {
		return "", fmt.Errorf("unsupported secret version: %d", secret.Version)
	}

	salt, err := base64.StdEncoding.DecodeString(secret.Salt)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(secret.Nonce)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return "", err
	}

	key := deriveSecretKey(salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("failed to decrypt saved key")
	}
	return string(plaintext), nil
}

func saveAPIKey(value string) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	key := deriveSecretKey(salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
	payload := storedSecret{
		Version:    1,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(appConfigDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(secretFilePath(), raw, 0o600)
}

func deriveSecretKey(salt []byte) []byte {
	return pbkdf2SHA256([]byte(machineBindingMaterial()), salt, 120000, 32)
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hashLen := 32
	blocks := (keyLen + hashLen - 1) / hashLen
	out := make([]byte, 0, blocks*hashLen)
	for block := 1; block <= blocks; block++ {
		out = append(out, pbkdf2Block(password, salt, iter, block)...)
	}
	return out[:keyLen]
}

func pbkdf2Block(password, salt []byte, iter, blockNum int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{byte(blockNum >> 24), byte(blockNum >> 16), byte(blockNum >> 8), byte(blockNum)})
	u := mac.Sum(nil)

	t := make([]byte, len(u))
	copy(t, u)
	for i := 1; i < iter; i++ {
		mac = hmac.New(sha256.New, password)
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return t
}

func machineBindingMaterial() string {
	home, _ := os.UserHomeDir()
	hostname, _ := os.Hostname()
	parts := []string{
		appID,
		runtime.GOOS,
		runtime.GOARCH,
		strings.TrimSpace(os.Getenv("USER")),
		strings.TrimSpace(os.Getenv("USERNAME")),
		home,
		hostname,
		readFirstExistingFile("/etc/machine-id", "/var/lib/dbus/machine-id"),
	}
	return strings.Join(parts, "|")
}

func readFirstExistingFile(paths ...string) string {
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(raw))
		}
	}
	return ""
}

func secretFilePath() string {
	return filepath.Join(appConfigDir(), "openrouter_key.enc.json")
}

func configCandidates() []string {
	candidates := []string{}
	if value := strings.TrimSpace(os.Getenv("CCODE_CONFIG")); value != "" {
		candidates = append(candidates, value)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "config.json"))
	}
	candidates = append(candidates, filepath.Join(appConfigDir(), "config.json"))
	return dedupePaths(candidates)
}

func loadEnvFiles() {
	for _, path := range envCandidates() {
		applyEnvFile(path)
	}
}

func envCandidates() []string {
	candidates := []string{}
	if value := strings.TrimSpace(os.Getenv("CCODE_ENV_FILE")); value != "" {
		candidates = append(candidates, value)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, ".env"))
	}
	candidates = append(candidates, filepath.Join(appConfigDir(), "ccode.env"))
	return dedupePaths(candidates)
}

func applyEnvFile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func appConfigDir() string {
	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		return filepath.Join(base, defaultConfigDir)
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", defaultConfigDir)
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, defaultConfigDir)
		}
	}
	return filepath.Join(home, ".config", defaultConfigDir)
}

func firstExistingFile(paths []string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func dedupePaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		cleaned := filepath.Clean(strings.TrimSpace(path))
		if cleaned == "." || cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func handleUnset() {
	for _, key := range managedEnvVars {
		if runtime.GOOS == "windows" {
			fmt.Printf("Remove-Item Env:%s -ErrorAction SilentlyContinue\n", key)
		} else {
			fmt.Printf("unset %s\n", key)
		}
	}
}

func promptYesNo(label string, defaultYes bool) (bool, error) {
	defaultValue := "y"
	if !defaultYes {
		defaultValue = "n"
	}
	answer, err := promptDefault(label+" [y/n]", defaultValue)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, errors.New("please answer y or n")
	}
}

func shellExportLines(envs map[string]string) string {
	keys := make([]string, 0, len(envs))
	for key := range envs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		if runtime.GOOS == "windows" {
			lines = append(lines, "$env:"+key+" = "+powershellQuote(envs[key]))
		} else {
			lines = append(lines, "export "+key+"="+shellQuote(envs[key]))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func flattenEnvMap(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+envMap[key])
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powershellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func shellSplit(text string) ([]string, error) {
	var parts []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range text {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("failed to parse launch command")
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts, nil
}

func claudeSearchPaths() []string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
		"/usr/bin/claude",
	}
	if runtime.GOOS == "windows" {
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		candidates = append(candidates,
			filepath.Join(appData, "npm", "claude.cmd"),
			filepath.Join(appData, "npm", "claude"),
			filepath.Join(localAppData, "Programs", "Claude", "claude.exe"),
		)
	}
	return dedupePaths(candidates)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func isInteractiveTerminal() bool {
	if runtime.GOOS == "windows" {
		consoleIn, errIn := os.Open("CONIN$")
		if errIn == nil {
			consoleIn.Close()
		}
		consoleOut, errOut := os.OpenFile("CONOUT$", os.O_RDWR, 0)
		if errOut == nil {
			consoleOut.Close()
		}
		return errIn == nil && errOut == nil
	}
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	stdoutInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stdinInfo.Mode()&os.ModeCharDevice) != 0 && (stdoutInfo.Mode()&os.ModeCharDevice) != 0
}

func promptDefault(label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	}
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
