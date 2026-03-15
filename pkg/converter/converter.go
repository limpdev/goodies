package converter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

// ProcessDirectoryRecursively walks a directory and converts .html/.htm files to .md
func ProcessDirectoryRecursively(dirPath string) error {
	return filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		lowerCasePath := strings.ToLower(path)
		if strings.HasSuffix(lowerCasePath, ".html") || strings.HasSuffix(lowerCasePath, ".htm") {
			fmt.Fprintf(os.Stderr, "Processing file: %s\n", path)
			htmlBytes, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error reading file %s: %v\n", path, err)
				return nil
			}

			markdown, err := HTMLToMarkdown(string(htmlBytes))
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error converting file %s: %v\n", path, err)
				return nil
			}

			if markdown == "" && len(htmlBytes) > 0 {
				fmt.Fprintf(os.Stderr, "  Skipping write for %s due to empty conversion.\n", path)
				return nil
			}

			ext := filepath.Ext(path)
			baseName := path[0 : len(path)-len(ext)]
			outputPath := baseName + ".md"

			err = os.WriteFile(outputPath, []byte(markdown), 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error writing markdown file %s: %v\n", outputPath, err)
				return nil
			}
			fmt.Fprintf(os.Stderr, "  Successfully converted to: %s\n", outputPath)
		}
		return nil
	})
}

// HTMLToMarkdown performs the actual conversion using the library
func HTMLToMarkdown(html string) (string, error) {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			strikethrough.NewStrikethroughPlugin(),
			table.NewTablePlugin(),
		),
	)
	markdown, err := conv.ConvertString(html)
	if err != nil {
		return "", fmt.Errorf("error during HTML to Markdown conversion: %w", err)
	}
	return markdown, nil
}
