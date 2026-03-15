# Goodies: The All-in-One Web Scraper & HTML-to-Markdown Converter

Golly & Gong are two peas in a pod, which is to say, I've been too lazy to merge them into a properly powerful HTTP fetching, HTML formulating, and Markdown generating pure Golang web-scraper. Golly scrapes the link, returns HTML  Gong converts that raw HTML into purified Markdown. Surprisingly, Gong in particular has operated considerably better than **literally every other solution I've tried from within the realm of HTML-to-Markdown conversion**. I didn't intend on building the most feugo, supreme Markdown generator _(it probably isn't I know)_, but no shit it works better than the other shit I've tried.

Both project summaries are found in their respective XML files, aptly named. 

---

**Goodies** is a powerful Go-based command-line tool that combines web scraping capabilities with HTML-to-Markdown conversion. It can scrape web pages with fine-grained control, extract specific content using CSS selectors, and convert HTML content (from web pages or local files) into clean Markdown format.

## Features

- 🌐 **Web Scraping**: Fetch and extract content from websites
- 🎯 **CSS Selector Targeting**: Extract specific DOM elements using CSS selectors
- 📁 **Local File Processing**: Convert local HTML files to Markdown
- 📂 **Batch Processing**: Recursively convert entire directories of HTML files
- 🔄 **Multiple Output Formats**: JSON, plain text, raw content, HTML, complete HTML (with inlined resources), and Markdown
- ⚡ **Concurrent Scraping**: Parallel processing with configurable limits
- 🎨 **Resource Inlining**: Automatically inline CSS and JavaScript for complete HTML output
- 📝 **Flexible Input**: Accept URLs, local files, or stdin input
- 🛡️ **Robust Error Handling**: Graceful error recovery and logging

## Installation

### Prerequisites
- Go 1.16 or higher

### From Source
```bash
git clone <repository-url>
cd goodies
go build -o goodies
```

### Using go install
```bash
go install github.com/your-username/goodies@latest
```

## Usage

### Basic Syntax
```bash
goodies [flags] <URL|FILE|DIR>
```

### Flags
| Flag | Description | Default |
|------|-------------|---------|
| `-o` | Output file path | (stdout) |
| `-r` | Recursively process directories (Markdown conversion mode only) | false |
| `-s` | CSS selector to target (e.g., 'article', '#content') | "" |
| `-a` | User Agent string | "Mozilla/5.0 (Compatible; Goodies/1.0)" |
| `-f` | Output format: `complete`, `html`, `text`, `json`, `raw`, `md` | `complete` |

## Usage Examples

### 1. Basic Web Scraping

**Scrape a webpage and output complete HTML (default):**
```bash
goodies https://example.com
```

**Scrape with a custom user agent:**
```bash
goodies -a "MyBot/1.0" https://example.com
```

### 2. Targeted Content Extraction

**Extract only the article content:**
```bash
goodies -s "article" https://news-site.com/article-123
```

**Extract content from a specific div:**
```bash
goodies -s "#main-content" https://blog.example.com/post
```

### 3. Different Output Formats

**JSON output (full structured data):**
```bash
goodies -f json https://example.com
```

**Plain text output:**
```bash
goodies -f text https://example.com
```

**Raw content only:**
```bash
goodies -f raw https://example.com
```

**Original HTML:**
```bash
goodies -f html https://example.com
```

**Complete HTML (with inlined resources):**
```bash
goodies -f complete https://example.com
```

### 4. Convert to Markdown

**Scrape and convert to Markdown:**
```bash
goodies -f md https://example.com/blog/post
```

**Scrape specific content and convert to Markdown:**
```bash
goodies -s ".post-content" -f md https://example.com/blog/post
```

**Save output to file:**
```bash
goodies -f md https://example.com -o article.md
```

### 5. Local File Processing

**Convert a local HTML file to Markdown:**
```bash
goodies file.html -f md
```

**Convert with selector targeting:**
```bash
goodies -s "#content" -f md local-file.html
```

**Save to specific output file:**
```bash
goodies file.html -f md -o converted.md
```

### 6. Batch Processing

**Recursively convert all HTML files in a directory:**
```bash
goodies -r ./docs
```

This will find all `.html` and `.htm` files in the `./docs` directory and create corresponding `.md` files.

### 7. Using STDIN

**Pipe HTML content from another command:**
```bash
curl -s https://example.com | goodies -f md
```

**Process HTML from a variable:**
```bash
cat input.html | goodies -f md -o output.md
```

### 8. Advanced Scraping Scenarios

**Scrape multiple pages (programmatically by modifying URLs list):**
*(Note: The current implementation supports multiple URLs in the configuration, though the CLI accepts single input)*

**Extract all links from a page:**
```bash
goodies -f json https://example.com | jq '.Links'
```

**Extract images from a page:**
```bash
goodies -f json https://example.com | jq '.Images'
```

## Output Formats Explained

### `json`
Returns a structured JSON object containing:
- URL, title, status code
- Extracted content
- All links and images found
- HTML structure (head, body, full)
- CSS and JavaScript references
- Timestamp

### `text`
Human-readable formatted text with section headers and extracted content.

### `raw`
Only the text content extracted from the targeted selector.

### `html`
The original HTML of the targeted element or full page.

### `complete`
A complete HTML document with external CSS and JavaScript resources inlined, making it self-contained.

### `md`
Markdown format converted from the HTML content using `html-to-markdown` library with commonmark extensions.

## Advanced Configuration

While the CLI provides basic options, you can customize the scraper further by modifying the `GollyArgs` structure in code:

```go
config := &GollyArgs{
    URLs:           []string{"https://example.com"},
    UserAgent:      "CustomBot/1.0",
    Headers:        map[string]string{"Authorization": "Bearer token"},
    Delay:          2 * time.Second,
    Parallelism:    5,
    TargetSelector: ".content",
    OutputFormat:   "md",
    EnableDebug:    true,
    AllowedDomains: []string{"example.com"},
}
```

## Error Handling

- Failed scrapes are logged but don't stop processing of other URLs
- Missing selectors generate warnings but continue processing
- Invalid URLs or network errors are reported with details
- File system errors in batch mode are reported per file

## Performance Tips

1. **Use appropriate delays** when scraping multiple pages to avoid overloading servers
2. **Limit parallelism** for sensitive targets
3. **Use selectors** to extract only needed content, reducing memory usage
4. **For batch processing**, ensure sufficient system resources for large directories

## Limitations

- JavaScript-rendered content cannot be scraped (static HTML only)
- Rate limiting is basic; respect websites' `robots.txt` manually
- Very large pages may cause memory issues
- Complex CSS selectors might not work as expected with the HTML-to-Markdown conversion

## Development

### Dependencies
- `github.com/gocolly/colly/v2` - Web scraping framework
- `github.com/PuerkitoBio/goquery` - jQuery-like HTML parsing
- `github.com/JohannesKaufmann/html-to-markdown/v2` - HTML to Markdown conversion

### Building and Testing
```bash
# Build
go build -o goodies

# Run tests (if available)
go test ./...

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o goodies-linux
GOOS=windows GOARCH=amd64 go build -o goodies.exe
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes with appropriate tests
4. Submit a pull request

## License

[Specify license here]

## Support

For issues and feature requests, please use the issue tracker on the repository.

---

**Goodies** combines the power of Go's concurrency with robust scraping and conversion libraries, making it an ideal tool for content migration, archiving, and web data extraction tasks.