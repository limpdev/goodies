package cmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"goodies/pkg/converter"
	"goodies/pkg/scraper"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	outputFile string
	recursive  bool
	selector   string
	userAgent  string
	format     string
	cookieFile string
)

var rootCmd = &cobra.Command{
	Use:   "goodies [URL|FILE|DIR] [command]",
	Short: "Goodies: The All-in-One Scraper & Converter",
	Long: `Goodies allows you to scrape web pages, resources, and convert files.

Examples:
  goodies https://example.com -f md
  goodies batch urls.txt -d ./out
  goodies setbinary "C:\Program Files\Google\Chrome\Application\chrome.exe"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRoot,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recursively process directories")
	rootCmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector to target")
	rootCmd.Flags().StringVarP(&userAgent, "user-agent", "a",
		"Mozilla/5.0 (Compatible; Goodies/1.0)", "User Agent string")
	rootCmd.Flags().StringVarP(&format, "format", "f", "complete",
		"Output format: complete|html|text|json|raw|md")

	rootCmd.PersistentFlags().StringVarP(&cookieFile, "cookies", "c", "",
		"Path to cookie file (JSON or Netscape format)")

	// Env var override: GOODIES_CHROME_BIN=/usr/bin/chromium
	viper.SetEnvPrefix("GOODIES")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
}

func initConfig() {
	viper.SetConfigName(".goodies")
	viper.SetConfigType("yaml")
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(home)
	}
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintf(os.Stderr, "Using config: %s\n", viper.ConfigFileUsed())
	}
}

// getChromeBin resolves the Chrome binary path from the priority chain:
//
//	env (GOODIES_CHROME_BIN) > config file (chrome.bin) > "" (disabled)
//
// An empty return value means JS rendering is disabled — the scraper will
// fall back to static HTML for any JS-heavy page.
func getChromeBin() string {
	return viper.GetString("chrome.bin")
}

func runRoot(cmd *cobra.Command, args []string) error {
	timeStart := time.Now()
	defer func() {
		fmt.Fprintf(os.Stderr, "Done in %v\n", time.Since(timeStart))
	}()

	if recursive {
		if len(args) < 1 {
			return fmt.Errorf("error: directory path required for recursive mode")
		}
		return converter.ProcessDirectoryRecursively(args[0])
	}

	var inputData string
	var isURL bool
	var sourceName string

	if len(args) > 0 {
		sourceName = args[0]
		if strings.HasPrefix(sourceName, "http://") || strings.HasPrefix(sourceName, "https://") {
			isURL = true
		} else {
			content, err := os.ReadFile(sourceName)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", sourceName, err)
			}
			inputData = string(content)
		}
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			bytes, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("error reading stdin: %w", err)
			}
			inputData = string(bytes)
			sourceName = "stdin"
		} else {
			return cmd.Help()
		}
	}

	scraperFormat := format
	if format == "md" {
		scraperFormat = "complete"
	}

	var finalOutput string

	if isURL {
		fmt.Fprintf(os.Stderr, "Scraping %s...\n", sourceName)
		u, _ := url.Parse(sourceName)
		config := &scraper.GollyArgs{
			URLs:           []string{sourceName},
			UserAgent:      userAgent,
			TargetSelector: selector,
			AllowedDomains: []string{u.Hostname()},
			Parallelism:    2,
			OutputFormat:   scraperFormat,
			CookieFile:     cookieFile,
			ChromeBin:      getChromeBin(),
		}
		s := scraper.NewScraper(config)
		defer s.Close()

		if err := s.Scrape(); err != nil {
			return err
		}
		if len(s.Results) == 0 {
			return fmt.Errorf("no data scraped")
		}
		result := s.Results[0]
		finalOutput = s.GetFormattedString(result, scraperFormat)
	} else {
		finalOutput = inputData
	}

	if format == "md" {
		fmt.Fprintf(os.Stderr, "Converting to Markdown...\n")
		var err error
		finalOutput, err = converter.HTMLToMarkdown(finalOutput)
		if err != nil {
			return err
		}
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(finalOutput), 0644); err != nil {
			return fmt.Errorf("error writing output: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Written to %s\n", outputFile)
	} else {
		fmt.Println(finalOutput)
	}
	return nil
}
