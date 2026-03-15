package cmd

import (
	"bufio"
	"fmt"
	"goodies/pkg/converter"
	"goodies/pkg/scraper"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var outputDir string

var batchCmd = &cobra.Command{
	Use:   "batch [FILE]",
	Short: "Batch process a list of URLs from a file",
	Long: `Reads a file containing a list of URLs (one per line) and processes them 
according to the global flags (format, selector, cookies, etc.).
Output files are saved to the current directory or the directory specified by --output-dir.`,
	Args: cobra.ExactArgs(1),
	RunE: runBatch,
}

func init() {
	batchCmd.Flags().StringVarP(&outputDir, "output-dir", "d", ".", "Directory to save output files")
	// Register the command with root
	rootCmd.AddCommand(batchCmd)
}

func runBatch(cmd *cobra.Command, args []string) error {
	inputFile := args[0]
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("could not open batch file: %w", err)
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if len(urls) == 0 {
		return fmt.Errorf("no valid URLs found in file %s", inputFile)
	}

	fmt.Printf("Loaded %d URLs from %s\n", len(urls), inputFile)

	// Determine effective scraper format
	scraperFormat := format
	if format == "md" {
		scraperFormat = "complete"
	}

	// Prepare scraper args
	// Note: We don't set AllowedDomains effectively allowing all, which is desired for a batch list of different sites.
	config := &scraper.GollyArgs{
		URLs:           urls,
		UserAgent:      userAgent,
		TargetSelector: selector,
		Parallelism:    2,
		OutputFormat:   scraperFormat,
		CookieFile:     cookieFile, // Use the global cookie flag
	}

	fmt.Println("Starting scraper...")
	s := scraper.NewScraper(config)
	if err := s.Scrape(); err != nil {
		return err
	}

	if len(s.Results) == 0 {
		return fmt.Errorf("no results obtained")
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	successCount := 0
	for _, result := range s.Results {
		// 1. Get content
		finalOutput := s.GetFormattedString(result, scraperFormat)

		// 2. Convert if needed
		if format == "md" {
			var err error
			finalOutput, err = converter.HTMLToMarkdown(finalOutput)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error converting content for %s: %v\n", result.URL, err)
				continue
			}
		}

		// 3. Determine filename
		safeName := sanitizeFilename(result.URL)
		fileName := fmt.Sprintf("%s.%s", safeName, format)
		outputPath := filepath.Join(outputDir, fileName)

		// 4. Write
		if err := os.WriteFile(outputPath, []byte(finalOutput), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to %s: %v\n", outputPath, err)
		} else {
			fmt.Printf("Saved: %s\n", outputPath)
			successCount++
		}
	}

	fmt.Printf("Batch processing complete. %d/%d files saved.\n", successCount, len(urls))
	return nil
}

func sanitizeFilename(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		// Fallback for invalid URLs
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '_'
		}, urlStr)
		if len(clean) > 50 {
			return clean[:50]
		}
		return clean
	}

	// Use hostname + path
	name := u.Hostname() + u.Path
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, ".", "_")

	// Truncate if too long
	if len(name) > 100 {
		name = name[:100]
	}
	// Handle empty case
	if name == "" || name == "_" {
		name = "index"
	}
	return name
}
