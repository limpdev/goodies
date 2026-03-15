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
)

// Flag variables
var (
	outputFile string
	recursive  bool
	selector   string
	userAgent  string
	format     string
	cookieFile string // New variable
)

var rootCmd = &cobra.Command{
	Use:   "goodies [URL|FILE|DIR] [command]",
	Short: "Goodies: The All-in-One Scraper & Converter",
	Long: `Goodies allows you to scrape web pages, extract content, 
inline resources, and convert HTML to Markdown.
Examples:
  goodies https://example.com -f md              # Scrape & Convert to Markdown
  goodies batch urls.txt -d ./out                # Batch process URLs
  goodies https://example.com -c cookies.txt     # Scrape with cookies`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRoot,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Define flags
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recursively process directories (Markdown conversion mode only)")
	rootCmd.Flags().StringVarP(&selector, "selector", "s", "", "CSS selector to target (e.g. 'article', '#content')")
	rootCmd.Flags().StringVarP(&userAgent, "user-agent", "a", "Mozilla/5.0 (Compatible; Goodies/1.0)", "User Agent string")
	rootCmd.Flags().StringVarP(&format, "format", "f", "complete", "Output format: complete|html|text|json|raw|md")

	// New persistent flag for cookies (applies to root and batch)
	rootCmd.PersistentFlags().StringVarP(&cookieFile, "cookies", "c", "", "Path to cookie file (JSON or Netscape format)")
}

func runRoot(cmd *cobra.Command, args []string) error {
	timeStart := time.Now()
	defer func() {
		fmt.Fprintf(os.Stderr, "Done in %v\n", time.Since(timeStart))
	}()

	// 1. Handle Recursive Directory Mode
	if recursive {
		if len(args) < 1 {
			return fmt.Errorf("error: directory path required for recursive mode")
		}
		dirPath := args[0]
		// Recursive mode implies Markdown conversion by default
		if err := converter.ProcessDirectoryRecursively(dirPath); err != nil {
			return fmt.Errorf("recursive processing failed: %w", err)
		}
		return nil
	}

	// 2. Determine Input Source
	var inputData string
	var isURL bool
	var sourceName string

	if len(args) > 0 {
		sourceName = args[0]
		if strings.HasPrefix(sourceName, "http://") || strings.HasPrefix(sourceName, "https://") {
			isURL = true
		} else {
			// Read from File
			content, err := os.ReadFile(sourceName)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", sourceName, err)
			}
			inputData = string(content)
		}
	} else {
		// Read from Stdin
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

	// 3. Execution Pipeline
	var finalOutput string

	// Determine effective scraper format
	// If the user wants markdown ("md"), the scraper needs to generate "complete" HTML first
	scraperFormat := format
	if format == "md" {
		scraperFormat = "complete"
	}

	if isURL {
		// --- PHASE 1: SCRAPE ---
		fmt.Fprintf(os.Stderr, "Scraping %s...\n", sourceName)
		u, _ := url.Parse(sourceName)
		config := &scraper.GollyArgs{
			URLs:           []string{sourceName},
			UserAgent:      userAgent,
			TargetSelector: selector,
			AllowedDomains: []string{u.Hostname()},
			Parallelism:    2,
			OutputFormat:   scraperFormat,
			CookieFile:     cookieFile, // Pass cookie file
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
		// Input was already HTML file or Stdin
		finalOutput = inputData
	}

	// --- PHASE 2: CONVERT ---
	// If the requested format is "md", we pipe the HTML (from scraper or file) through converter
	if format == "md" {
		fmt.Fprintf(os.Stderr, "Converting to Markdown...\n")
		var err error
		finalOutput, err = converter.HTMLToMarkdown(finalOutput)
		if err != nil {
			return err
		}
	}

	// 4. Output
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
