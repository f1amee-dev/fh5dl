package book

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/ztrue/tracerr"
)

var idRegex = regexp.MustCompile(`^(\w+\/\w+)\/?`)
var startTrimPattern = regexp.MustCompile(`^[^\{]+`)
var endTrimPattern = regexp.MustCompile(`[^}]+$`)

type Book struct {
	Url   string
	Id    string
	Title string
	Pages []Page
}

type Page struct {
	Number       int
	ThumbnailUrl string
	ImageUrls    []string
}

type PageImage struct {
	PageNumber   int
	ImageNumber  int
	OverallOrder int
	Url          string
}

type DownloadedImage struct {
	PageNumber   int
	ImageNumber  int
	OverallOrder int
	Url          string
	FullPath     string
}

type htmlConfig struct {
	Pages []page `json:"fliphtml5_pages"`
	Meta  meta   `json:"meta"`
}

type meta struct {
	Title string `json:"title"`
}

type page struct {
	Images   interface{} `json:"n"`
	ThumbUrl string      `json:"t"`
}

// interactivePageImage represents a screenshot of a page with all interactive elements visible
type InteractivePageImage struct {
	PageNumber   int
	OverallOrder int
	Url          string
	FullPath     string
}

// revealInteractiveElementsScript is the javascript code to reveal all hidden texts and click all interactive elements
const RevealInteractiveElementsScript = `
(() => {
  // use requestAnimationFrame for better performance
  return new Promise(resolve => {
    requestAnimationFrame(() => {
      let revealedTextElementsCount = 0;
      let clickedRectangleCount = 0;
      let foundRectangleTriggers = 0;

      // --- part 1: selectively reveal TEXT elements hidden by opacity: 0 ---
      // optimize selector to be more specific
      const potentialTextElements = document.querySelectorAll('[id^="E+_Text_"], .leo-comp--txt');
      
      // use for-loop instead of forEach for better performance
      for (let i = 0; i < potentialTextElements.length; i++) {
        const el = potentialTextElements[i];
        const style = window.getComputedStyle(el);
        if (style.opacity === '0') {
          el.style.opacity = '1';
          // make robustly visible
          if (style.visibility === 'hidden') {
            el.style.visibility = 'visible';
          }
          if (style.display === 'none') {
             el.style.display = ''; // or 'block', 'inline', etc.
          }
          revealedTextElementsCount++;
        }
      }

      // --- part 2: find and click RECTANGLE triggers ---
      const rectangleTriggers = document.querySelectorAll('[id^="E+_Rectangle_"], .leo-comp--shape-rect.leo-action-trigger');
      foundRectangleTriggers = rectangleTriggers.length;

      // use for-loop instead of forEach for better performance
      for (let i = 0; i < rectangleTriggers.length; i++) {
        const rect = rectangleTriggers[i];
        let originalOpacity = rect.style.opacity;
        let originalVisibility = rect.style.visibility;
        let originalDisplay = rect.style.display;

        let computedStyle = window.getComputedStyle(rect);

        // make it just interactable enough for a click
        let madeTemporarilyInteractable = false;
        if (computedStyle.opacity === '0') {
            rect.style.opacity = '0.01';
            madeTemporarilyInteractable = true;
        }
        if (computedStyle.visibility === 'hidden') {
            rect.style.visibility = 'visible';
            madeTemporarilyInteractable = true;
        }
        if (computedStyle.display === 'none') {
            rect.style.display = 'block';
            madeTemporarilyInteractable = true;
        }

        if (typeof rect.click === 'function') {
          try {
            rect.click();
            clickedRectangleCount++;
          } catch (e) {
            console.error("Error clicking rectangle trigger:", rect.id || rect.className, e);
          }
        }

        // if we temporarily made it interactable, revert those changes
        if (madeTemporarilyInteractable) {
            rect.style.opacity = originalOpacity;
            rect.style.visibility = originalVisibility;
            rect.style.display = originalDisplay;
        }
      }
      
      resolve(true);
    });
  });
})()
`

// captureInteractivePage captures a screenshot of a page with all interactive elements revealed
func CaptureInteractivePage(ctx context.Context, pageUrl string, outputFolder string, pageNumber int, overallOrder int) (*InteractivePageImage, error) {
	fmt.Printf("Starting to capture page %d from URL: %s\n", pageNumber, pageUrl)

	// we need to adjust our javascript based on whether this is an odd or even page number
	// for flipHTML5 books, page 1 is single, then 2-3 are together, 4-5 together, etc.
	isFirstPage := pageNumber == 1
	isRightPage := pageNumber%2 == 0 // even numbered pages are on the right side of spreads

	// full path for the screenshot
	fullPath := filepath.Join(outputFolder, fmt.Sprintf("interactive-%d.png", pageNumber))

	// first check if the file already exists to avoid duplicate work
	if _, err := os.Stat(fullPath); err == nil {
		fmt.Printf("Screenshot for page %d already exists, skipping...\n", pageNumber)
		return &InteractivePageImage{
			PageNumber:   pageNumber,
			OverallOrder: overallOrder,
			Url:          pageUrl,
			FullPath:     fullPath,
		}, nil
	}

	// create a new chrome instance with optimized options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		// add performance flags
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-component-extensions-with-background-pages", true),
		chromedp.Flag("disable-features", "TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("enable-automation", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("blink-settings", "imagesEnabled=true"),
		chromedp.Flag("disable-notifications", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("js-flags", "--max_old_space_size=512"),
		chromedp.WindowSize(1920, 1080),
	)

	// Properly manage Chrome instances to avoid race conditions
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	// Create browser context with a more robust approach
	chromeCtx, chromeCancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithLogf(func(format string, args ...interface{}) {
			// Silencing verbose chromedp logs
			if false { // Only enable for debugging
				fmt.Printf("[ChromeDP] "+format+"\n", args...)
			}
		}),
	)
	defer chromeCancel()

	// Set a more reasonable timeout
	timeoutCtx, timeoutCancel := context.WithTimeout(chromeCtx, 60*time.Second)
	defer timeoutCancel()

	// Maximum number of retries
	maxRetries := 2
	var err error
	var buf []byte

	// Retry loop
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry attempt %d for page %d\n", attempt, pageNumber)
			time.Sleep(time.Second * 2)
		}

		// Use a single Run call for the entire process to reduce race conditions
		err = chromedp.Run(timeoutCtx,
			// First navigate to the page
			chromedp.Navigate(pageUrl),

			// Wait for the page to load
			chromedp.Sleep(3*time.Second),

			// Execute our reveal script to show hidden elements
			chromedp.EvaluateAsDevTools(`
			(() => {
				// Find and make all text elements visible
				document.querySelectorAll('[id^="E+_Text_"], .leo-comp--txt').forEach(el => {
					if (window.getComputedStyle(el).opacity === '0') {
						el.style.opacity = '1';
						if (window.getComputedStyle(el).visibility === 'hidden') {
							el.style.visibility = 'visible';
						}
						if (window.getComputedStyle(el).display === 'none') {
							el.style.display = '';
						}
					}
				});
				
				// Find and click all rectangle triggers
				document.querySelectorAll('[id^="E+_Rectangle_"], .leo-comp--shape-rect.leo-action-trigger').forEach(rect => {
					try {
						let needsTemp = false;
						if (window.getComputedStyle(rect).opacity === '0') {
							rect.style.opacity = '0.01';
							needsTemp = true;
						}
						if (rect.click) {
							rect.click();
						}
						// Don't revert opacity - keep the results visible
					} catch (e) {
						console.error("Error clicking element:", e);
					}
				});
				
				return "Revealed hidden elements";
			})()
			`, nil),

			// Wait for triggers to take effect
			chromedp.Sleep(1*time.Second),

			// Execute JavaScript to focus and isolate just the target page from the spread
			chromedp.EvaluateAsDevTools(fmt.Sprintf(`
			(() => {
				// Use a single style element instead of modifying each element individually
				// Create the style element first
				const style = document.createElement('style');
				document.head.appendChild(style);
				
				// UI element selectors to hide
				const uiElementSelectors = [
					// Specific IDs for FlipHTML5 UI
					'#fbTopBar', '#fbToolBar',
					
					// Classes from the FlipHTML5 UI structure
					'.fbTopBar', '.logoBar', '.topRightBar', '.searchBar', '.fbToolBar', '.buttonBar', '.pageBar',
					
					// General UI selectors
					'.toolbar', '.navbar', '.nav', 'header', '.header', '.flipbook-bar', 
					'.menu', '.button', '.btn', '.control', '.navigation', '.flipbook-menu',
					'.flipbook-nav', '.flipbook-ui', '.ui-element', '[class*="menu"]', 
					'[class*="toolbar"]', '[class*="button"]', '[class*="control"]',
					'[class*="nav"]', '.app-header', '.app-footer', '.footer',
					'#toolbar', '#menu', '#header', '#footer', '.zoom-panel',
					'#appFooter', '#loadingFooter', '.hint', '.loading', '.bookLoading',
					'.top-menu', '.bottom-menu', '.controls', '.thumbnails', '#toolbar', '#header',
					'.fixed-top', '.fixed-bottom',
					'.ms-control', '.ms-toolbar', '.btn-toolbar',
					'.flip-book-toolbar', '.flipbook-container .toolbar'
				];
				
				// Build CSS rules in a single string for better performance
				let styleContent = '';
				for (let i = 0; i < uiElementSelectors.length; i++) {
					styleContent += uiElementSelectors[i] + ' { display: none !important; visibility: hidden !important; opacity: 0 !important; pointer-events: none !important; height: 0 !important; width: 0 !important; overflow: hidden !important; position: absolute !important; z-index: -1000 !important; }\n';
				}
				
				// Apply all CSS at once
				style.textContent = styleContent;
				
				// Get the pages with optimized selectors
				let currentPages = Array.from(document.querySelectorAll('.leo-page, .flipbook-page, .page-elem, .flipbook-page3d, [class*="page"]'))
					.filter(page => {
						const style = window.getComputedStyle(page);
						const rect = page.getBoundingClientRect();
						
						return style.display !== 'none' && 
							   style.visibility !== 'hidden' && 
							   style.opacity !== '0' &&
							   parseInt(style.zIndex || 0) > 0 &&
							   rect.width > 100 && 
							   rect.height > 100;
					});
				
				// Get the page number and isRightPage from outside the JavaScript
				const pageNumber = %d;
				const isRightPage = %s;
				const isFirstPage = %s;
				
				// Short circuit for faster processing
				if (isFirstPage === "true" && currentPages.length > 0) {
					// For first page, just use the first visible page and make it fullscreen
					const page = currentPages[0];
					page.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "First page prepared for screenshot";
				}
				else if (currentPages.length >= 2) {
					// In paired view, figure out which one we want (left or right)
					// Sort pages by position (left to right)
					currentPages.sort((a, b) => a.getBoundingClientRect().left - b.getBoundingClientRect().left);
					
					// Select left (0) or right (1) page based on page number
					const targetPage = isRightPage === "true" ? currentPages[1] : currentPages[0];
					targetPage.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "Page spread prepared for screenshot";
				}
				else if (currentPages.length === 1) {
					// If there's only one page visible, use it
					const page = currentPages[0];
					page.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "Single page prepared for screenshot";
				}
				else {
					// Backup case
					if (currentPages.length > 0) {
						const bestPage = currentPages[0];
						bestPage.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
						document.body.style.background = 'white';
						document.documentElement.style.background = 'white';
					}
					return "Fallback page layout prepared";
				}
			})()
			`, pageNumber,
				fmt.Sprintf("%t", isRightPage),
				fmt.Sprintf("%t", isFirstPage)), nil),

			// Wait for isolation to apply
			chromedp.Sleep(1*time.Second),

			// Take a full screenshot
			chromedp.FullScreenshot(&buf, 100),
		)

		// If successful, break the retry loop
		if err == nil && len(buf) > 0 {
			break
		}

		// Log error but continue retrying
		if err != nil {
			fmt.Printf("Error during capture for page %d (attempt %d): %v\n", pageNumber, attempt+1, err)
		}
	}

	// If we still have an error after all retries
	if err != nil {
		return nil, tracerr.Wrap(fmt.Errorf("error taking screenshot for page %d after %d attempts: %w", pageNumber, maxRetries, err))
	}

	// If buf is empty, we never successfully took a screenshot
	if len(buf) == 0 {
		return nil, tracerr.Wrap(fmt.Errorf("failed to capture screenshot for page %d after %d attempts", pageNumber, maxRetries))
	}

	fmt.Printf("Screenshot for page %d captured successfully\n", pageNumber)

	// Save the screenshot to disk
	err = os.WriteFile(fullPath, buf, 0644)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	return &InteractivePageImage{
		PageNumber:   pageNumber,
		OverallOrder: overallOrder,
		Url:          pageUrl,
		FullPath:     fullPath,
	}, nil
}

// CaptureInteractivePageQuiet is a version of CaptureInteractivePage with reduced log output
func CaptureInteractivePageQuiet(ctx context.Context, pageUrl string, outputFolder string, pageNumber int, overallOrder int) (*InteractivePageImage, error) {
	// Only output minimal logs
	fmt.Printf(".") // Just a simple progress indicator

	// We need to adjust our JavaScript based on whether this is an odd or even page number
	// For FlipHTML5 books, page 1 is single, then 2-3 are together, 4-5 together, etc.
	isFirstPage := pageNumber == 1
	isRightPage := pageNumber%2 == 0 // even numbered pages are on the right side of spreads

	// Full path for the screenshot
	fullPath := filepath.Join(outputFolder, fmt.Sprintf("interactive-%d.png", pageNumber))

	// First check if the file already exists to avoid duplicate work
	if _, err := os.Stat(fullPath); err == nil {
		return &InteractivePageImage{
			PageNumber:   pageNumber,
			OverallOrder: overallOrder,
			Url:          pageUrl,
			FullPath:     fullPath,
		}, nil
	}

	// Create a new Chrome instance with optimized options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		// Add performance flags
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-component-extensions-with-background-pages", true),
		chromedp.Flag("disable-features", "TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("enable-automation", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("blink-settings", "imagesEnabled=true"),
		chromedp.Flag("disable-notifications", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("js-flags", "--max_old_space_size=512"),
		chromedp.WindowSize(1920, 1080),
	)

	// Properly manage Chrome instances to avoid race conditions
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	// Create browser context with a more robust approach
	chromeCtx, chromeCancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithLogf(func(format string, args ...interface{}) {
			// Silencing verbose chromedp logs
			if false { // Only enable for debugging
				fmt.Printf("[ChromeDP] "+format+"\n", args...)
			}
		}),
	)
	defer chromeCancel()

	// Set a more reasonable timeout
	timeoutCtx, timeoutCancel := context.WithTimeout(chromeCtx, 60*time.Second)
	defer timeoutCancel()

	// Maximum number of retries
	maxRetries := 2
	var err error
	var buf []byte

	// Retry loop
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("r") // 'r' for retry
			time.Sleep(time.Second * 2)
		}

		// Use a single Run call for the entire process to reduce race conditions
		err = chromedp.Run(timeoutCtx,
			// First navigate to the page
			chromedp.Navigate(pageUrl),

			// Wait for the page to load
			chromedp.Sleep(3*time.Second),

			// Execute our reveal script to show hidden elements
			chromedp.EvaluateAsDevTools(`
			(() => {
				// Find and make all text elements visible
				document.querySelectorAll('[id^="E+_Text_"], .leo-comp--txt').forEach(el => {
					if (window.getComputedStyle(el).opacity === '0') {
						el.style.opacity = '1';
						if (window.getComputedStyle(el).visibility === 'hidden') {
							el.style.visibility = 'visible';
						}
						if (window.getComputedStyle(el).display === 'none') {
							el.style.display = '';
						}
					}
				});
				
				// Find and click all rectangle triggers
				document.querySelectorAll('[id^="E+_Rectangle_"], .leo-comp--shape-rect.leo-action-trigger').forEach(rect => {
					try {
						let needsTemp = false;
						if (window.getComputedStyle(rect).opacity === '0') {
							rect.style.opacity = '0.01';
							needsTemp = true;
						}
						if (rect.click) {
							rect.click();
						}
						// Don't revert opacity - keep the results visible
					} catch (e) {
						console.error("Error clicking element:", e);
					}
				});
				
				return "Revealed hidden elements";
			})()
			`, nil),

			// Wait for triggers to take effect
			chromedp.Sleep(1*time.Second),

			// Execute JavaScript to focus and isolate just the target page from the spread
			chromedp.EvaluateAsDevTools(fmt.Sprintf(`
			(() => {
				// Use a single style element instead of modifying each element individually
				// Create the style element first
				const style = document.createElement('style');
				document.head.appendChild(style);
				
				// UI element selectors to hide
				const uiElementSelectors = [
					// Specific IDs for FlipHTML5 UI
					'#fbTopBar', '#fbToolBar',
					
					// Classes from the FlipHTML5 UI structure
					'.fbTopBar', '.logoBar', '.topRightBar', '.searchBar', '.fbToolBar', '.buttonBar', '.pageBar',
					
					// General UI selectors
					'.toolbar', '.navbar', '.nav', 'header', '.header', '.flipbook-bar', 
					'.menu', '.button', '.btn', '.control', '.navigation', '.flipbook-menu',
					'.flipbook-nav', '.flipbook-ui', '.ui-element', '[class*="menu"]', 
					'[class*="toolbar"]', '[class*="button"]', '[class*="control"]',
					'[class*="nav"]', '.app-header', '.app-footer', '.footer',
					'#toolbar', '#menu', '#header', '#footer', '.zoom-panel',
					'#appFooter', '#loadingFooter', '.hint', '.loading', '.bookLoading',
					'.top-menu', '.bottom-menu', '.controls', '.thumbnails', '#toolbar', '#header',
					'.fixed-top', '.fixed-bottom',
					'.ms-control', '.ms-toolbar', '.btn-toolbar',
					'.flip-book-toolbar', '.flipbook-container .toolbar'
				];
				
				// Build CSS rules in a single string for better performance
				let styleContent = '';
				for (let i = 0; i < uiElementSelectors.length; i++) {
					styleContent += uiElementSelectors[i] + ' { display: none !important; visibility: hidden !important; opacity: 0 !important; pointer-events: none !important; height: 0 !important; width: 0 !important; overflow: hidden !important; position: absolute !important; z-index: -1000 !important; }\n';
				}
				
				// Apply all CSS at once
				style.textContent = styleContent;
				
				// Get the pages with optimized selectors
				let currentPages = Array.from(document.querySelectorAll('.leo-page, .flipbook-page, .page-elem, .flipbook-page3d, [class*="page"]'))
					.filter(page => {
						const style = window.getComputedStyle(page);
						const rect = page.getBoundingClientRect();
						
						return style.display !== 'none' && 
							   style.visibility !== 'hidden' && 
							   style.opacity !== '0' &&
							   parseInt(style.zIndex || 0) > 0 &&
							   rect.width > 100 && 
							   rect.height > 100;
					});
				
				// Get the page number and isRightPage from outside the JavaScript
				const pageNumber = %d;
				const isRightPage = %s;
				const isFirstPage = %s;
				
				// Short circuit for faster processing
				if (isFirstPage === "true" && currentPages.length > 0) {
					// For first page, just use the first visible page and make it fullscreen
					const page = currentPages[0];
					page.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "First page prepared for screenshot";
				}
				else if (currentPages.length >= 2) {
					// In paired view, figure out which one we want (left or right)
					// Sort pages by position (left to right)
					currentPages.sort((a, b) => a.getBoundingClientRect().left - b.getBoundingClientRect().left);
					
					// Select left (0) or right (1) page based on page number
					const targetPage = isRightPage === "true" ? currentPages[1] : currentPages[0];
					targetPage.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "Page spread prepared for screenshot";
				}
				else if (currentPages.length === 1) {
					// If there's only one page visible, use it
					const page = currentPages[0];
					page.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
					document.body.style.background = 'white';
					document.documentElement.style.background = 'white';
					return "Single page prepared for screenshot";
				}
				else {
					// Backup case
					if (currentPages.length > 0) {
						const bestPage = currentPages[0];
						bestPage.style.cssText = "position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:9999;";
						document.body.style.background = 'white';
						document.documentElement.style.background = 'white';
					}
					return "Fallback page layout prepared";
				}
			})()
			`, pageNumber,
				fmt.Sprintf("%t", isRightPage),
				fmt.Sprintf("%t", isFirstPage)), nil),

			// Wait for isolation to apply
			chromedp.Sleep(1*time.Second),

			// Take a full screenshot
			chromedp.FullScreenshot(&buf, 100),
		)

		// If successful, break the retry loop
		if err == nil && len(buf) > 0 {
			break
		}

		// Log error but continue retrying
		if err != nil {
			// Just log a compact message for errors
			fmt.Printf("e") // 'e' for error
		}
	}

	// If we still have an error after all retries
	if err != nil {
		return nil, tracerr.Wrap(fmt.Errorf("error capturing page %d after %d attempts: %w", pageNumber, maxRetries, err))
	}

	// If buf is empty, we never successfully took a screenshot
	if len(buf) == 0 {
		return nil, tracerr.Wrap(fmt.Errorf("failed to capture page %d after %d attempts", pageNumber, maxRetries))
	}

	// Show a success indicator
	fmt.Printf("+") // '+' for success

	// Save the screenshot to disk
	err = os.WriteFile(fullPath, buf, 0644)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	return &InteractivePageImage{
		PageNumber:   pageNumber,
		OverallOrder: overallOrder,
		Url:          pageUrl,
		FullPath:     fullPath,
	}, nil
}

func ParseId(idOrUrl string) (string, error) {
	// First, check if the given string already looks like an ID (e.g. "abcde/fg123")
	if matches := idRegex.FindStringSubmatch(idOrUrl); matches != nil && len(matches) >= 2 {
		return matches[1], nil
	}

	// Try to parse it as a URL and extract the path components
	if u, err := url.Parse(idOrUrl); err == nil && u.Host != "" {
		// Trim leading and trailing slashes from the path
		trimmedPath := strings.Trim(u.Path, "/")
		// The ID in a FlipHTML5 URL is always the first two path segments: <account>/<book>
		matches := idRegex.FindStringSubmatch(trimmedPath)
		if matches != nil && len(matches) >= 2 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid ID or URL: %s", idOrUrl)
}

func downloadHtmlConfig(id string) (*htmlConfig, error) {
	response, err := http.Get(fmt.Sprintf("https://online.fliphtml5.com/%s/javascript/config.js", id))
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download book information: %s", response.Status)
	}

	jsConfigBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	jsConfig := string(jsConfigBytes)
	jsonConfig := startTrimPattern.ReplaceAllLiteralString(jsConfig, "")
	jsonConfig = endTrimPattern.ReplaceAllLiteralString(jsonConfig, "")

	var config htmlConfig
	err = json.Unmarshal([]byte(jsonConfig), &config)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	return &config, nil
}

func Get(idOrUrl string) (*Book, error) {
	id, err := ParseId(idOrUrl)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	htmlConfig, err := downloadHtmlConfig(id)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	pages := make([]Page, 0)
	for i, pageInfo := range htmlConfig.Pages {
		images := make([]string, 0)

		// Handle different types of Images field
		switch v := pageInfo.Images.(type) {
		case []interface{}:
			for _, img := range v {
				if imgStr, ok := img.(string); ok {
					// Clean leading "./" which appears in some configs
					trimmed := strings.TrimPrefix(imgStr, "./")
					// If the path already starts with "files/" it is a full relative path, otherwise assume it's just the filename.
					if strings.HasPrefix(trimmed, "files/") {
						images = append(images, fmt.Sprintf("https://online.fliphtml5.com/%s/%s", id, trimmed))
					} else {
						images = append(images, fmt.Sprintf("https://online.fliphtml5.com/%s/files/large/%s", id, trimmed))
					}
				}
			}
		case string:
			// Clean leading "./" which appears in some configs
			trimmed := strings.TrimPrefix(v, "./")
			// If the path already starts with "files/" it is a full relative path, otherwise assume it's just the filename.
			if strings.HasPrefix(trimmed, "files/") {
				images = append(images, fmt.Sprintf("https://online.fliphtml5.com/%s/%s", id, trimmed))
			} else {
				images = append(images, fmt.Sprintf("https://online.fliphtml5.com/%s/files/large/%s", id, trimmed))
			}
		}

		pages = append(pages, Page{
			Number:       i + 1,
			ThumbnailUrl: pageInfo.ThumbUrl,
			ImageUrls:    images,
		})
	}

	return &Book{
		Url:   fmt.Sprintf("https://online.fliphtml5.com/%s/", id),
		Id:    id,
		Title: html.UnescapeString(htmlConfig.Meta.Title),
		Pages: pages,
	}, nil
}

func (b *Book) FindAllImages() []PageImage {
	images := make([]PageImage, 0)

	order := 1
	for i, page := range b.Pages {
		for j, imageUrl := range page.ImageUrls {
			images = append(images, PageImage{
				PageNumber:   i + 1,
				ImageNumber:  j + 1,
				OverallOrder: order,
				Url:          imageUrl,
			})

			order++
		}
	}

	return images
}

func (i *PageImage) Download(ctx context.Context, outputFolder string) (*DownloadedImage, error) {
	fullPath := filepath.Join(outputFolder, fmt.Sprintf("%d-%d.jpg", i.PageNumber, i.ImageNumber))

	// Check if file already exists first to avoid unnecessary downloads
	if _, err := os.Stat(fullPath); err == nil {
		// File already exists, return it directly
		return &DownloadedImage{
			PageNumber:   i.PageNumber,
			ImageNumber:  i.ImageNumber,
			OverallOrder: i.OverallOrder,
			Url:          i.Url,
			FullPath:     fullPath,
		}, nil
	}

	// Create a custom client with optimized timeouts
	client := &http.Client{
		Timeout: 30 * time.Second, // Set a reasonable timeout
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false, // Keep compression enabled for faster downloads
			DisableKeepAlives:   false, // Keep connections alive for better performance
		},
	}

	// Max retries
	maxRetries := 3
	var lastErr error

	// Retry loop for resilience
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff for retries
			sleepTime := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(sleepTime)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.Url, nil)
		if err != nil {
			lastErr = err
			continue
		}

		// Add headers to make it look like a browser request
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		req.Header.Set("Accept-Encoding", "gzip, deflate")
		req.Header.Set("Connection", "keep-alive")

		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Ensure the response body is always closed
		if res.Body != nil {
			defer res.Body.Close()
		}

		if res.StatusCode != http.StatusOK {
			// Try alternative URL forms
			candidates := []string{}
			if strings.Contains(i.Url, "/files/large/") {
				candidates = append(candidates, strings.Replace(i.Url, "/files/large/", "/files/", 1))
			}
			if strings.HasSuffix(i.Url, ".webp") {
				base := strings.TrimSuffix(i.Url, ".webp")
				candidates = append(candidates, base+".jpg", base+".png")
			}
			for _, alt := range candidates {
				reqAlt, errAlt := http.NewRequestWithContext(ctx, http.MethodGet, alt, nil)
				if errAlt != nil {
					continue
				}
				resAlt, errAlt := client.Do(reqAlt)
				if errAlt == nil && resAlt.StatusCode == http.StatusOK {
					i.Url = alt
					res = resAlt
					goto OK
				}
			}
			altUrl := strings.Replace(i.Url, "/files/large/", "/files/", 1)
			// quick retry with alternate URL (no recursion, single attempt)
			reqAlt, errAlt := http.NewRequestWithContext(ctx, http.MethodGet, altUrl, nil)
			if errAlt == nil {
				resAlt, errAlt := client.Do(reqAlt)
				if errAlt == nil && resAlt.StatusCode == http.StatusOK {
					// swap URL and response for normal processing
					i.Url = altUrl
					res = resAlt
				} else {
					// fall through to continue retries
					lastErr = fmt.Errorf("failed to download image (status: %s)", res.Status)
					continue
				}
			}
			lastErr = fmt.Errorf("failed to download image (status: %s)", res.Status)
			continue
		}

	OK:
		// Create the output file
		file, err := os.Create(fullPath)
		if err != nil {
			lastErr = err
			continue
		}

		// Use a buffered copy for better performance
		bufWriter := bufio.NewWriter(file)
		_, err = io.Copy(bufWriter, res.Body)

		// Make sure to flush and close even if copy fails
		flushErr := bufWriter.Flush()
		closeErr := file.Close()

		if err != nil {
			// If the copy failed, handle it
			lastErr = err
			// Try to remove the potentially corrupted file
			os.Remove(fullPath)
			continue
		}

		if flushErr != nil {
			lastErr = flushErr
			os.Remove(fullPath)
			continue
		}

		if closeErr != nil {
			lastErr = closeErr
			os.Remove(fullPath)
			continue
		}

		// If we got here, download was successful
		return &DownloadedImage{
			PageNumber:   i.PageNumber,
			ImageNumber:  i.ImageNumber,
			OverallOrder: i.OverallOrder,
			Url:          i.Url,
			FullPath:     fullPath,
		}, nil
	}

	// If we exhausted all retries, return the last error
	return nil, tracerr.Wrap(fmt.Errorf("failed to download image after %d attempts: %w", maxRetries, lastErr))
}
