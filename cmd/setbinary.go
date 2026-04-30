package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var setBinaryCmd = &cobra.Command{
	Use:   "setbinary [PATH]",
	Short: "Set the Chrome/Chromium binary path for JS rendering",
	Long: `Writes the given Chrome or Chromium binary path into your ~/.goodies.yaml
config file under the chrome.bin key. This path is used as the headless
browser for JS-rendered pages.

The value persists across invocations. To disable JS rendering, run:
  goodies setbinary ""

Examples:
  goodies setbinary "/usr/bin/chromium"
  goodies setbinary "C:\Program Files\Google\Chrome\Application\chrome.exe"
  goodies setbinary "%USERPROFILE%\AppData\Local\Google\Chrome\Application\chrome.exe"`,
	Args: cobra.ExactArgs(1),
	RunE: runSetBinary,
}

func init() {
	rootCmd.AddCommand(setBinaryCmd)
}

func runSetBinary(cmd *cobra.Command, args []string) error {
	binPath := args[0]

	// Resolve the config file path — prefer the file Viper already loaded,
	// fall back to ~/.goodies.yaml so we always write to a predictable location.
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		configPath = filepath.Join(home, ".goodies.yaml")
	}

	// Read whatever is already in the file so we don't clobber other keys.
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		// Tolerate a missing file — we'll create it fresh below.
		_ = yaml.Unmarshal(data, &existing)
	}

	// Navigate/create the nested chrome.bin key.
	chromeSection, ok := existing["chrome"].(map[string]interface{})
	if !ok {
		chromeSection = make(map[string]interface{})
	}

	if binPath == "" {
		delete(chromeSection, "bin")
		// If the chrome section is now empty, remove it entirely.
		if len(chromeSection) == 0 {
			delete(existing, "chrome")
		} else {
			existing["chrome"] = chromeSection
		}
	} else {
		chromeSection["bin"] = binPath
		existing["chrome"] = chromeSection
	}

	// Marshal back to YAML and write.
	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, out, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", configPath, err)
	}

	if binPath == "" {
		fmt.Fprintf(os.Stderr, "Chrome binary path cleared in %s\n", configPath)
		fmt.Fprintln(os.Stderr, "JS rendering is now disabled.")
	} else {
		fmt.Fprintf(os.Stderr, "Chrome binary path saved to %s\n", configPath)
		fmt.Fprintf(os.Stderr, "  chrome.bin: %s\n", binPath)
	}
	return nil
}
