package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/debug"
)

type GollyArgs struct {
	URLs           []string          `json:"urls"`
	UserAgent      string            `json:"user_agent"`
	Headers        map[string]string `json:"headers"`
	Delay          time.Duration     `json:"delay"`
	Parallelism    int               `json:"parallelism"`
	TargetSelector string            `json:"target_selector"`
	OutputFormat   string            `json:"output_format"`
	EnableDebug    bool              `json:"enable_debug"`
	AllowedDomains []string          `json:"allowed_domains"`
	CookieFile     string            `json:"cookie_file"`
}

type ScrapedData struct {
	URL        string                 `json:"url"`
	Title      string                 `json:"title"`
	Content    map[string]interface{} `json:"content"`
	Attributes map[string]string      `json:"attributes"`
	Links      []string               `json:"links"`
	Images     []string               `json:"images"`
	Timestamp  time.Time              `json:"timestamp"`
	StatusCode int                    `json:"status_code"`
	HeadHTML   string                 `json:"head_html"`
	BodyHTML   string                 `json:"body_html"`
	FullHTML   string                 `json:"full_html"`
	CSS        []string               `json:"css"`
	InlineCSS  []string               `json:"inline_css"`
	JavaScript []string               `json:"javascript"`
	InlineJS   []string               `json:"inline_js"`
}

type Scraper struct {
	Config    *GollyArgs
	Collector *colly.Collector
	Results   []ScrapedData
	mu        sync.Mutex
	errors    []error // FIX (Bug 2): collect errors from callbacks
}

func NewScraper(config *GollyArgs) *Scraper {
	c := colly.NewCollector(
		colly.UserAgent(config.UserAgent),
		colly.Async(true),
	)

	if len(config.AllowedDomains) > 0 {
		c.AllowedDomains = config.AllowedDomains
	}

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: config.Parallelism,
		Delay:       config.Delay,
	})

	if config.EnableDebug {
		c.SetDebugger(&debug.LogDebugger{})
	}

	s := &Scraper{
		Config:    config,
		Collector: c,
		Results:   make([]ScrapedData, 0),
		errors:    make([]error, 0),
	}

	if config.CookieFile != "" {
		if err := s.loadAndSetCookies(config.CookieFile); err != nil {
			log.Printf("Warning: Failed to load cookies: %v", err)
		} else {
			log.Printf("Loaded cookies from %s", config.CookieFile)
		}
	}

	return s
}

func (s *Scraper) Scrape() error {
	s.setupCallbacks()

	// FIX (Bug 1): Check Visit() errors and handle redirect domains
	for _, urlStr := range s.Config.URLs {
		if err := s.Collector.Visit(urlStr); err != nil {
			log.Printf("Visit failed for %s: %v", urlStr, err)

			// FIX (Bug 6): If it's a domain issue, try adding www/non-www variant
			if err.Error() == "Forbidden domain" {
				log.Printf("Hint: The URL may redirect to a different domain. Check AllowedDomains.")
			}

			s.mu.Lock()
			s.errors = append(s.errors, fmt.Errorf("visit %s: %w", urlStr, err))
			s.mu.Unlock()
		}
	}

	s.Collector.Wait()

	// FIX (Bug 2): Surface errors if we got zero results
	if len(s.Results) == 0 && len(s.errors) > 0 {
		return fmt.Errorf("scrape failed — first error: %w", s.errors[0])
	}

	return nil
}

// -----------------------------------------------------------------------------
// PARSER PROCESSING LOGIC
// -----------------------------------------------------------------------------

func (s *Scraper) setupCallbacks() {
	// FIX (Bug 6): Follow redirects to other domains automatically
	s.Collector.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
		// Add the redirect target's hostname to AllowedDomains dynamically
		newHost := req.URL.Hostname()

		allowed := false
		for _, d := range s.Collector.AllowedDomains {
			if d == newHost {
				allowed = true
				break
			}
		}
		if !allowed {
			log.Printf("Adding redirect domain to AllowedDomains: %s", newHost)
			s.Collector.AllowedDomains = append(s.Collector.AllowedDomains, newHost)
		}

		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	})

	s.Collector.OnHTML("html", func(e *colly.HTMLElement) {
		// FIX (Bug 4): Recover from any panic inside the callback
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED panic in OnHTML callback for %s: %v", e.Request.URL, r)
				// Still try to parse the static DOM so we get SOMETHING
				data := s.parseDOM(e.DOM, e.Request.URL.String())
				s.mu.Lock()
				s.Results = append(s.Results, data)
				s.mu.Unlock()
			}
		}()

		reqURL := e.Request.URL.String()
		requiresJSFallback := false
		selection := e.DOM

		// 1. JS HEURISTICS
		if s.Config.TargetSelector != "" {
			if e.DOM.Find(s.Config.TargetSelector).Length() == 0 {
				requiresJSFallback = true
			}
		} else {
			// FIX (Bug 5): Raise threshold and require noscript/framework markers
			bodyText := strings.TrimSpace(e.DOM.Find("body").Text())
			hasJSFrameworkMarker := e.DOM.Find(`div#root, div#app, div#__next, div[data-reactroot]`).Length() > 0
			if len(bodyText) < 200 && hasJSFrameworkMarker {
				requiresJSFallback = true
			}
		}

		// 2. LIGHTPANDA WORKFLOW TRIGGER
		if requiresJSFallback {
			log.Printf("JS rendering needed for %s — handing off to Lightpanda...", reqURL)
			renderedHTML, err := s.fetchWithLightpanda(reqURL)
			if err != nil {
				log.Printf("Lightpanda fallback failed for %s: %v — using static HTML", reqURL, err)
			} else if renderedHTML != "" {
				doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(renderedHTML))
				if parseErr == nil {
					// Use doc.Find("html") to get the <html> element specifically,
					// matching what Colly's e.DOM provides
					htmlSel := doc.Find("html")
					if htmlSel.Length() > 0 {
						selection = htmlSel
					} else {
						selection = doc.Selection
					}
					log.Printf("Lightpanda successfully rendered %s", reqURL)
				} else {
					log.Printf("Failed to parse Lightpanda HTML for %s: %v", reqURL, parseErr)
				}
			}
		}

		// 3. UNIFIED DOM PARSING
		data := s.parseDOM(selection, reqURL)
		s.mu.Lock()
		s.Results = append(s.Results, data)
		s.mu.Unlock()
	})

	s.Collector.OnError(func(r *colly.Response, err error) {
		log.Printf("Error scraping %s (status %d): %v", r.Request.URL, r.StatusCode, err)
		// FIX (Bug 2): Capture the error so Scrape() can report it
		s.mu.Lock()
		s.errors = append(s.errors, fmt.Errorf("%s (status %d): %w", r.Request.URL, r.StatusCode, err))
		s.mu.Unlock()
	})
}

func (s *Scraper) parseDOM(doc *goquery.Selection, reqURL string) ScrapedData {
	data := ScrapedData{
		URL:        reqURL,
		Title:      doc.Find("title").Text(),
		Content:    make(map[string]interface{}),
		Attributes: make(map[string]string),
		Links:      make([]string, 0),
		Images:     make([]string, 0),
		Timestamp:  time.Now(),
	}

	baseURL, _ := url.Parse(reqURL)

	if head := doc.Find("head"); head.Length() > 0 {
		html, _ := head.Html()
		data.HeadHTML = html
	}

	doc.Find("link[rel='stylesheet']").Each(func(_ int, sel *goquery.Selection) {
		if href, exists := sel.Attr("href"); exists {
			data.CSS = append(data.CSS, s.resolveURL(baseURL, href))
		}
	})

	doc.Find("script[src]").Each(func(_ int, sel *goquery.Selection) {
		if src, exists := sel.Attr("src"); exists {
			data.JavaScript = append(data.JavaScript, s.resolveURL(baseURL, src))
		}
	})

	selection := doc
	if s.Config.TargetSelector != "" {
		selection = doc.Find(s.Config.TargetSelector)
		if selection.Length() == 0 {
			log.Printf("Warning: Selector '%s' not found on %s", s.Config.TargetSelector, reqURL)
		}
	}

	if html, err := selection.Html(); err == nil {
		if s.Config.TargetSelector != "" {
			data.BodyHTML = html
		} else {
			data.FullHTML = html
		}
		data.Content["text"] = strings.TrimSpace(selection.Text())
	}

	if selection.Is("a[href]") {
		if href, exists := selection.Attr("href"); exists {
			data.Links = append(data.Links, s.resolveURL(baseURL, href))
		}
	}
	selection.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		if href, exists := sel.Attr("href"); exists {
			data.Links = append(data.Links, s.resolveURL(baseURL, href))
		}
	})

	return data
}

// -----------------------------------------------------------------------------
// LIGHTPANDA CDP LOGIC
// -----------------------------------------------------------------------------

// FIX (Bug 3): Discover the correct WebSocket URL from Lightpanda's HTTP API
func (s *Scraper) discoverCDPEndpoint(baseURL string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	// Try /json/version first (browser-level WebSocket URL)
	resp, err := client.Get(baseURL + "/json/version")
	if err != nil {
		return "", fmt.Errorf("could not reach Lightpanda at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return "", fmt.Errorf("failed to parse /json/version: %w", err)
	}

	if versionInfo.WebSocketDebuggerURL != "" {
		log.Printf("Discovered Lightpanda WebSocket URL: %s", versionInfo.WebSocketDebuggerURL)
		return versionInfo.WebSocketDebuggerURL, nil
	}

	// Fallback: try /json to get a page-level endpoint
	resp2, err := client.Get(baseURL + "/json")
	if err != nil {
		return "", fmt.Errorf("failed to query /json: %w", err)
	}
	defer resp2.Body.Close()

	var targets []struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&targets); err != nil {
		return "", fmt.Errorf("failed to parse /json: %w", err)
	}

	if len(targets) > 0 && targets[0].WebSocketDebuggerURL != "" {
		log.Printf("Discovered Lightpanda page WebSocket URL: %s", targets[0].WebSocketDebuggerURL)
		return targets[0].WebSocketDebuggerURL, nil
	}

	return "", fmt.Errorf("no WebSocket URL found from Lightpanda at %s", baseURL)
}

func (s *Scraper) fetchWithLightpanda(targetURL string) (string, error) {
	// FIX (Bug 3): Proper endpoint discovery
	cdpURL := os.Getenv("LIGHTPANDA_CDP_URL")
	if cdpURL == "" {
		httpBase := os.Getenv("LIGHTPANDA_HTTP_URL")
		if httpBase == "" {
			httpBase = "http://127.0.0.1:9222"
		}

		discovered, err := s.discoverCDPEndpoint(httpBase)
		if err != nil {
			return "", fmt.Errorf("CDP endpoint discovery failed: %w", err)
		}
		cdpURL = discovered
	}

	log.Printf("Connecting to Lightpanda CDP at: %s", cdpURL)

	allocatorCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), cdpURL)
	defer cancel()

	ctx, cancelCtx := chromedp.NewContext(allocatorCtx)
	defer cancelCtx()

	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	var htmlContent string
	var actions []chromedp.Action

	// 1. INJECT COOKIES (only if we have them — skip if Lightpanda doesn't support it)
	collyCookies := s.Collector.Cookies(targetURL)
	if len(collyCookies) > 0 {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range collyCookies {
				expr := network.SetCookie(c.Name, c.Value).
					WithDomain(c.Domain).
					WithPath(c.Path).
					WithSecure(c.Secure).
					WithHTTPOnly(c.HttpOnly)

				if !c.Expires.IsZero() {
					exp := cdp.TimeSinceEpoch(c.Expires)
					expr = expr.WithExpires(&exp)
				}

				if err := expr.Do(ctx); err != nil {
					// FIX: Don't fail the whole operation if one cookie fails
					log.Printf("Warning: couldn't inject cookie '%s': %v", c.Name, err)
				}
			}
			return nil
		}))
	}

	// 2. NAVIGATE
	actions = append(actions, chromedp.Navigate(targetURL))

	// 3. WAIT
	if s.Config.TargetSelector != "" {
		actions = append(actions, chromedp.WaitVisible(s.Config.TargetSelector, chromedp.ByQuery))
	} else {
		actions = append(actions, chromedp.WaitReady("body", chromedp.ByQuery))
	}

	// 4. EXTRACT
	actions = append(actions, chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery))

	err := chromedp.Run(ctx, actions...)
	if err != nil {
		return "", fmt.Errorf("chromedp.Run failed: %w", err)
	}

	return htmlContent, nil
}

// -----------------------------------------------------------------------------
// COOKIE PROCESSING LOGIC
// -----------------------------------------------------------------------------

func (s *Scraper) loadAndSetCookies(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cookies []*http.Cookie
	if err := json.Unmarshal(content, &cookies); err != nil {
		cookies, err = parseNetscapeCookies(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse cookies (tried JSON and Netscape): %v", err)
		}
	}

	domainCookies := make(map[string][]*http.Cookie)
	for _, c := range cookies {
		if c.Domain != "" {
			d := strings.TrimPrefix(c.Domain, ".")
			domainCookies[d] = append(domainCookies[d], c)
		}
	}

	for domain, cs := range domainCookies {
		urlStr := "https://" + domain
		if err := s.Collector.SetCookies(urlStr, cs); err != nil {
			log.Printf("Warning: couldn't set cookies for %s: %v", domain, err)
		}
	}

	return nil
}

func parseNetscapeCookies(content string) ([]*http.Cookie, error) {
	var cookies []*http.Cookie
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 7 {
			expires, _ := strconv.ParseInt(parts[4], 10, 64)
			c := &http.Cookie{
				Domain:  parts[0],
				Path:    parts[2],
				Secure:  strings.ToUpper(parts[3]) == "TRUE",
				Expires: time.Unix(expires, 0),
				Name:    parts[5],
				Value:   parts[6],
			}
			cookies = append(cookies, c)
		}
	}
	return cookies, nil
}

// -----------------------------------------------------------------------------
// LOGIC HELPERS
// -----------------------------------------------------------------------------

func (s *Scraper) extractMediaAttribute(linkTag string) string {
	mediaRegex := regexp.MustCompile(`media\s*=\s*["']([^"']+)["']`)
	matches := mediaRegex.FindStringSubmatch(linkTag)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func (s *Scraper) fetchResource(url string) string {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", s.Config.UserAgent)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return string(content)
}

func (s *Scraper) processImports(cssContent, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return cssContent
	}
	importRegex := regexp.MustCompile(`@import\s+(?:url\()?["']?([^"';)]+)["']?(?:\))?[^;]*;`)
	return importRegex.ReplaceAllStringFunc(cssContent, func(match string) string {
		urlRegex := regexp.MustCompile(`@import\s+(?:url\()?["']?([^"';)]+)["']?`)
		urlMatches := urlRegex.FindStringSubmatch(match)
		if len(urlMatches) < 2 {
			return match
		}
		importURL := s.resolveURL(base, urlMatches[1])
		if importURL == "" {
			return match
		}
		importedCSS := s.fetchResource(importURL)
		if importedCSS == "" {
			return match
		}
		processedImportedCSS := s.processImports(importedCSS, importURL)
		return fmt.Sprintf("/* Imported from: %s */\n%s", importURL, processedImportedCSS)
	})
}

func (s *Scraper) resolveURL(base *url.URL, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return base.Scheme + ":" + href
	}
	if base == nil {
		return href
	}
	resolved, err := base.Parse(href)
	if err != nil {
		return ""
	}
	return resolved.String()
}

func (s *Scraper) buildCompleteHTML(result ScrapedData) string {
	var html strings.Builder
	if result.FullHTML != "" {
		return s.inlineExternalResources(result.FullHTML, result)
	}
	html.WriteString("<!DOCTYPE html>\n<html>\n")
	if result.HeadHTML != "" {
		html.WriteString("<head>\n<!-- Constructed by Goodies -->\n")
		html.WriteString(result.HeadHTML)
		html.WriteString("\n</head>\n")
	} else {
		html.WriteString("<head><meta charset='UTF-8'></head>")
	}
	html.WriteString("<body>\n")
	html.WriteString(fmt.Sprintf("<!-- Goodies Target: %s -->\n", s.Config.TargetSelector))
	if result.BodyHTML != "" {
		html.WriteString(result.BodyHTML)
	} else if txt, ok := result.Content["text"].(string); ok {
		html.WriteString(fmt.Sprintf("<pre>%s</pre>", txt))
	}
	html.WriteString("\n</body>\n</html>")
	return s.inlineExternalResources(html.String(), result)
}

func (s *Scraper) inlineExternalResources(htmlInput string, result ScrapedData) string {
	html := htmlInput
	html = s.inlineStylesheets(html, result.URL)
	for _, jsURL := range result.JavaScript {
		if jsContent := s.fetchResource(jsURL); jsContent != "" {
			scriptPattern := fmt.Sprintf(`<script[^>]*src\s*=\s*["|']%s["|'][^>]*></script>`, regexp.QuoteMeta(jsURL))
			re := regexp.MustCompile(scriptPattern)
			inlineJS := fmt.Sprintf("<script>\n%s\n</script>", jsContent)
			html = re.ReplaceAllLiteralString(html, inlineJS)
		}
	}
	return html
}

func (s *Scraper) inlineStylesheets(html, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return html
	}
	linkRegex := regexp.MustCompile(`<link\s+([^>]*\s+)?rel\s*=\s*["']stylesheet["']([^>]*\s+)?href\s*=\s*["']([^"']+)["'][^>]*>|<link\s+([^>]*\s+)?href\s*=\s*["']([^"']+)["']([^>]*\s+)?rel\s*=\s*["']stylesheet["'][^>]*>`)
	return linkRegex.ReplaceAllStringFunc(html, func(match string) string {
		return s.processLinkTagMatch(match, base)
	})
}

func (s *Scraper) processLinkTagMatch(match string, base *url.URL) string {
	hrefRegex := regexp.MustCompile(`href\s*=\s*["']([^"']+)["']`)
	hrefMatches := hrefRegex.FindStringSubmatch(match)
	if len(hrefMatches) < 2 {
		return match
	}
	absoluteURL := s.resolveURL(base, hrefMatches[1])
	cssContent := s.fetchResource(absoluteURL)
	if cssContent == "" {
		return match
	}
	processedCSS := s.processImports(cssContent, absoluteURL)
	mediaAttr := s.extractMediaAttribute(match)
	styleTagOpen := "<style>"
	if mediaAttr != "" && mediaAttr != "all" {
		styleTagOpen = fmt.Sprintf(`<style media="%s">`, mediaAttr)
	}
	return fmt.Sprintf("%s\n/* Inlined: %s */\n%s\n</style>", styleTagOpen, absoluteURL, processedCSS)
}

func (s *Scraper) GetFormattedString(result ScrapedData, format string) string {
	switch format {
	case "json":
		jsonData, err := json.MarshalIndent(s.Results, "", "  ")
		if err != nil {
			return fmt.Sprintf("Error marshaling JSON: %v", err)
		}
		return string(jsonData)
	case "text":
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("=== %s ===\n", result.URL))
		sb.WriteString(fmt.Sprintf("Title: %s\n", result.Title))
		for key, value := range result.Content {
			sb.WriteString(fmt.Sprintf("--- %s ---\n%v\n", strings.ToUpper(key), value))
		}
		return sb.String()
	case "raw":
		if textContent, exists := result.Content["text"]; exists {
			return fmt.Sprintf("%v", textContent)
		}
		return ""
	case "html":
		return result.FullHTML
	case "complete":
		return s.buildCompleteHTML(result)
	case "md":
		return s.buildCompleteHTML(result)
	default:
		return s.buildCompleteHTML(result)
	}
}
