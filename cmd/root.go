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
	outputFile    string
	recursive     bool
	selector      string
	userAgent     string
	format        string
	cookieFile    string
	lightpandaURL string // NEW
)

var rootCmd = &cobra.Command{
	Use:   "goodies [URL|FILE|DIR] [command]",
	Short: "Goodies: The All-in-One Scraper & Converter",
	Long: `Goodies allows you to scrape web pages, extract content, 
inline resources, and convert HTML to Markdown.

Examples:
  goodies https://example.com -f md
  goodies batch urls.txt -d ./out
  goodies https://example.com --lightpanda-url http://100.78.42.10:9222`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRoot,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Initialize config BEFORE flag parsing
	cobra.OnInitialize(initConfig)

	// Local flags
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recursively process directories")
	rootCmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector to target")
	rootCmd.Flags().StringVarP(&userAgent, "user-agent", "a",
		"Mozilla/5.0 (Compatible; Goodies/1.0)", "User Agent string")
	rootCmd.Flags().StringVarP(&format, "format", "f", "complete",
		"Output format: complete|html|text|json|raw|md")

	// Persistent flags (available to all subcommands including batch)
	rootCmd.PersistentFlags().StringVarP(&cookieFile, "cookies", "c", "",
		"Path to cookie file (JSON or Netscape format)")
	rootCmd.PersistentFlags().StringVar(&lightpandaURL, "lightpanda-url", "",
		"Lightpanda HTTP base URL (e.g. http://100.x.y.z:9222)")

	// Bind the flag to Viper so config file and env vars also work
	viper.BindPFlag("lightpanda.url", rootCmd.PersistentFlags().Lookup("lightpanda-url"))

	// Env var: GOODIES_LIGHTPANDA_URL=http://100.x.y.z:9222
	viper.SetEnvPrefix("GOODIES")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
}

func initConfig() {
	// Look for config in home directory and current directory
	viper.SetConfigName(".goodies")
	viper.SetConfigType("yaml")

	// Search paths (in order)
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(home)
	}
	viper.AddConfigPath(".")

	// Read config — not an error if it doesn't exist
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintf(os.Stderr, "Using config: %s\n", viper.ConfigFileUsed())
	}
}

// getLightpandaURL resolves the Lightpanda URL from the priority chain
func getLightpandaURL() string {
	// Viper already handles the priority:
	//   flag > env (GOODIES_LIGHTPANDA_URL) > config file
	if u := viper.GetString("lightpanda.url"); u != "" {
		return u
	}
	// Final fallback
	return "http://127.0.0.1:9222"
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

	var finalOutput string
	scraperFormat := format
	if format == "md" {
		scraperFormat = "complete"
	}

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
			LightpandaURL:  getLightpandaURL(), // NEW: pass resolved URL
		}

		s := scraper.NewScraper(config)
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
