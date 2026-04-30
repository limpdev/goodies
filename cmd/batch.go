package cmd

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"goodies/pkg/converter"
	"goodies/pkg/scraper"

	"github.com/spf13/cobra"
)

var (
	outputDir string
	workers   int
)

var batchCmd = &cobra.Command{
	Use:   "batch [FILE]",
	Short: "Batch process a list of URLs from a file",
	Long: `Reads a file containing a list of URLs (one per line) and scrapes each one,
writing output files as soon as each URL finishes rather than waiting for the
full batch to complete.

Failed URLs produce a .err file alongside the output files.

Examples:
  goodies batch urls.txt -d ./out
  goodies batch urls.txt -d ./out -f md --workers 8`,
	Args: cobra.ExactArgs(1),
	RunE: runBatch,
}

func init() {
	batchCmd.Flags().StringVarP(&outputDir, "output-dir", "d", ".", "Directory to save output files")
	batchCmd.Flags().IntVar(&workers, "workers", 4, "Number of concurrent scrape workers")
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

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	scraperFormat := format
	if format == "md" {
		scraperFormat = "complete"
	}

	config := &scraper.GollyArgs{
		URLs:           urls,
		UserAgent:      userAgent,
		TargetSelector: selector,
		Parallelism:    workers,
		OutputFormat:   scraperFormat,
		CookieFile:     cookieFile,
		ChromeBin:      getChromeBin(),
	}

	s := scraper.NewScraper(config)
	defer s.Close()

	results := make(chan scraper.ScrapedData, workers)
	errs := make(chan scraper.ScrapeError, workers)

	// Track write outcomes for the final summary.
	var (
		mu           sync.Mutex
		successCount int
		failCount    int
	)

	// Writer goroutine — drains the results channel and writes files immediately.
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for result := range results {
			finalOutput := s.GetFormattedString(result, scraperFormat)

			if format == "md" {
				converted, convErr := converter.HTMLToMarkdown(finalOutput)
				if convErr != nil {
					writeErrFile(outputDir, result.URL, convErr)
					mu.Lock()
					failCount++
					mu.Unlock()
					continue
				}
				finalOutput = converted
			}

			safeName := sanitizeFilename(result.URL)
			outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.%s", safeName, format))

			if writeErr := os.WriteFile(outputPath, []byte(finalOutput), 0644); writeErr != nil {
				writeErrFile(outputDir, result.URL, writeErr)
				mu.Lock()
				failCount++
				mu.Unlock()
			} else {
				fmt.Printf("Saved: %s\n", outputPath)
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}
	}()

	// Error goroutine — drains the errors channel and writes .err files immediately.
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for scrapeErr := range errs {
			writeErrFile(outputDir, scrapeErr.URL, scrapeErr.Err)
			mu.Lock()
			failCount++
			mu.Unlock()
		}
	}()

	// ScrapeChan blocks until all URLs are visited and both channels are closed.
	fmt.Println("Starting batch scrape...")
	s.ScrapeChan(results, errs)

	// Wait for both drain goroutines to finish writing.
	writerWg.Wait()

	fmt.Printf("\nBatch complete — %d saved, %d failed (see .err files).\n", successCount, failCount)
	return nil
}

// writeErrFile writes a .err file for the given URL into outputDir.
func writeErrFile(outputDir, rawURL string, err error) {
	safeName := sanitizeFilename(rawURL)
	errPath := filepath.Join(outputDir, safeName+".err")
	content := fmt.Sprintf("URL: %s\nError: %v\n", rawURL, err)
	if writeErr := os.WriteFile(errPath, []byte(content), 0644); writeErr != nil {
		fmt.Fprintf(os.Stderr, "Could not write error file for %s: %v\n", rawURL, writeErr)
	} else {
		fmt.Fprintf(os.Stderr, "Error recorded: %s\n", errPath)
	}
}

func sanitizeFilename(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
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
	name := u.Hostname() + u.Path
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "")
	name = strings.ReplaceAll(name, ".", "_")
	if len(name) > 100 {
		name = name[:100]
	}
	if name == "" || name == "_" {
		name = "index"
	}
	return name
}
