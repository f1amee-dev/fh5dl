package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	arg "github.com/alexflint/go-arg"
	pdfcpu_api "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/schollz/progressbar/v3"
	book "github.com/ygunayer/fh5dl/internal/book"
	"github.com/ztrue/tracerr"
	"golang.org/x/sync/errgroup"
	// terminal ui imports
)

type Args struct {
	Url               string `arg:"positional" help:"ID or URL of the PDF to download"`
	Concurrency       int    `arg:"-c" help:"(Optional) Number of concurrent downloads. Defaults to (number of CPUs available - 1)"`
	OutputFolder      string `arg:"-o" help:"(Optional) Output folder for the PDF. Defaults to the current working directory" default:"."`
	ImageOutputFolder string `arg:"--image-out" help:"(Optional) Output folder for downloaded images. Defaults to a temporary directory" default:""`
	Force             bool   `arg:"-f" help:"(Optional) Overwrite existing PDF file if it exists"`
	Interactive       bool   `arg:"-i" help:"(Optional) Capture screenshots with interactive elements revealed"`
	TerminalUI        bool   `arg:"-t, --termui" help:"(Optional) Use the terminal UI instead of command line arguments"`
	BatchSize         int    `arg:"-b" help:"(Optional) Batch size for interactive captures. Defaults to 8" default:"8"`
}

func downloadImages(ctx context.Context, args *Args, images []book.PageImage) ([]book.DownloadedImage, error) {
	imageOutputRoot := ""
	if args.ImageOutputFolder != "" {
		realdir, err := filepath.Abs(args.ImageOutputFolder)
		if err != nil {
			return nil, tracerr.Wrap(err)
		}

		if _, err := os.Stat(realdir); os.IsNotExist(err) {
			err = os.MkdirAll(realdir, os.ModePerm)
			if err != nil {
				return nil, tracerr.Wrap(err)
			}
		}

		imageOutputRoot = realdir
	} else {
		tmpdir, err := os.MkdirTemp("", "fh5dl-")
		if err != nil {
			return nil, tracerr.Wrap(err)
		}

		imageOutputRoot = tmpdir
	}

	// use a more efficient method for large downloads
	downloadedImages := make([]book.DownloadedImage, 0, len(images))
	mutex := sync.Mutex{}

	// for better memory management, process in batches
	batchSize := 50 // smaller batches for more frequent updates
	if len(images) <= batchSize {
		batchSize = len(images)
	}

	numBatches := (len(images) + batchSize - 1) / batchSize // ceiling division

	// if more than 200 images, show more detailed progress
	if len(images) > 200 {
		fmt.Printf("Processing %d images in %d batches of %d\n", len(images), numBatches, batchSize)
	}

	mainBar := progressbar.NewOptions(len(images),
		progressbar.OptionSetDescription("Downloading images"),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWidth(50),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)

	// track download speeds
	startTime := time.Now()
	var completedImages int32

	for batchIdx := 0; batchIdx < numBatches; batchIdx++ {
		start := batchIdx * batchSize
		end := (batchIdx + 1) * batchSize
		if end > len(images) {
			end = len(images)
		}

		batchImages := images[start:end]

		// log batch progress
		if numBatches > 1 {
			fmt.Printf("Batch %d/%d: %d images\n", batchIdx+1, numBatches, len(batchImages))
		}

		eg, batchCtx := errgroup.WithContext(ctx)
		eg.SetLimit(args.Concurrency)

		for _, image := range batchImages {
			image := image // create copy for closure

			eg.Go(func() error {
				// first check if the file already exists to avoid unnecessary network requests
				expectedPath := filepath.Join(imageOutputRoot, fmt.Sprintf("%d-%d.jpg", image.PageNumber, image.ImageNumber))
				if _, err := os.Stat(expectedPath); err == nil {
					// file already exists
					mutex.Lock()
					downloadedImages = append(downloadedImages, book.DownloadedImage{
						PageNumber:   image.PageNumber,
						ImageNumber:  image.ImageNumber,
						OverallOrder: image.OverallOrder,
						Url:          image.Url,
						FullPath:     expectedPath,
					})
					mutex.Unlock()

					atomic.AddInt32(&completedImages, 1)
					if err := mainBar.Add(1); err != nil {
						return tracerr.Wrap(err)
					}

					return nil
				}

				// download the image if it doesn't exist
				result, err := image.Download(batchCtx, imageOutputRoot)
				if err != nil {
					return tracerr.Wrap(err)
				}

				mutex.Lock()
				downloadedImages = append(downloadedImages, *result)
				mutex.Unlock()

				// update progress and stats
				completed := atomic.AddInt32(&completedImages, 1)
				if completed%10 == 0 && completed > 0 {
					// calculate download speed and eta
					elapsed := time.Since(startTime)
					imagesPerSecond := float64(completed) / elapsed.Seconds()
					if imagesPerSecond > 0 {
						eta := time.Duration(float64(len(images)-int(completed))/imagesPerSecond) * time.Second
						fmt.Printf("\rRate: %.1f img/s, ETA: %s",
							imagesPerSecond, formatDuration(eta))
					}
				}

				if err := mainBar.Add(1); err != nil {
					return tracerr.Wrap(err)
				}

				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return nil, tracerr.Wrap(err)
		}

		// force gc between batches to reduce memory pressure
		runtime.GC()
	}

	if err := mainBar.Close(); err != nil {
		return nil, tracerr.Wrap(err)
	}

	// sort images by order
	sort.Slice(downloadedImages, func(i, j int) bool {
		return downloadedImages[i].OverallOrder < downloadedImages[j].OverallOrder
	})

	// final report
	fmt.Printf("Downloaded %d images in %s\n", len(downloadedImages),
		formatDuration(time.Since(startTime)))

	return downloadedImages, nil
}

func captureInteractivePages(ctx context.Context, args *Args, b *book.Book) ([]book.InteractivePageImage, error) {
	interactiveOutputRoot := ""
	if args.ImageOutputFolder != "" {
		realdir, err := filepath.Abs(args.ImageOutputFolder)
		if err != nil {
			return nil, tracerr.Wrap(err)
		}

		// Add an "interactive" subfolder
		interactiveOutputRoot = filepath.Join(realdir, "interactive")
		if _, err := os.Stat(interactiveOutputRoot); os.IsNotExist(err) {
			err = os.MkdirAll(interactiveOutputRoot, os.ModePerm)
			if err != nil {
				return nil, tracerr.Wrap(err)
			}
		}
	} else {
		tmpdir, err := os.MkdirTemp("", "fh5dl-interactive-")
		if err != nil {
			return nil, tracerr.Wrap(err)
		}

		interactiveOutputRoot = tmpdir
	}

	// Use a moderate concurrency for browser operations
	// Default to 4 for better throughput while still being memory efficient
	concurrencyLimit := 4 // Increased from 2 to 4
	if args.Concurrency > 0 && args.Concurrency < concurrencyLimit {
		concurrencyLimit = args.Concurrency
	}

	// Larger batch size while keeping concurrency controlled
	batchSize := 8 // Default batch size
	if args.BatchSize > 0 {
		batchSize = args.BatchSize // Use provided batch size if set
	}
	if batchSize < concurrencyLimit {
		batchSize = concurrencyLimit // Ensure batch size is at least as large as concurrency
	}

	fmt.Printf("Using concurrency limit of %d with batch size of %d for interactive captures\n", concurrencyLimit, batchSize)

	// Create a list of pages we actually need to capture
	// In FlipHTML5 books, usually page 1 is single, then 2-3 are together, 4-5 together, etc.
	// So we need to capture pages 1, 2, 4, 6, 8, ... since odd pages (except 1) can be extracted from the even page spread
	pagesToCapture := []int{1} // Always start with page 1 (single page)

	for i := 2; i <= len(b.Pages); i += 2 {
		// Add even numbered pages (2, 4, 6, 8...)
		pagesToCapture = append(pagesToCapture, i)
	}

	fmt.Printf("Optimized page capture: Will capture %d pages instead of %d (first page + even pages for spreads)\n", len(pagesToCapture), len(b.Pages))

	// Process pages in batches for better resource management
	numBatches := (len(pagesToCapture) + batchSize - 1) / batchSize // Ceiling division

	capturedPages := make([]book.InteractivePageImage, 0)
	failedPages := make([]int, 0)
	mutex := sync.Mutex{}

	// Used for time estimation
	startTime := time.Now()
	var completedPages int32 = 0
	totalPages := len(pagesToCapture)

	// Process batches sequentially but pages within each batch in parallel
	for batchIndex := 0; batchIndex < numBatches; batchIndex++ {
		startIdx := batchIndex * batchSize
		endIdx := (batchIndex + 1) * batchSize
		if endIdx > len(pagesToCapture) {
			endIdx = len(pagesToCapture)
		}

		currentBatch := pagesToCapture[startIdx:endIdx]
		fmt.Printf("Processing batch %d/%d with %d pages\n", batchIndex+1, numBatches, len(currentBatch))

		// Configure progress bar with timing estimate
		batchBar := progressbar.NewOptions(len(currentBatch),
			progressbar.OptionSetDescription(fmt.Sprintf("Batch %d/%d", batchIndex+1, numBatches)),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]=[reset]",
				SaucerHead:    "[green]>[reset]",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionOnCompletion(func() {
				fmt.Printf("\n")
			}),
			progressbar.OptionSetElapsedTime(true),
			progressbar.OptionFullWidth(),
		)

		// Create a fresh context for each batch
		batchCtx, batchCancel := context.WithCancel(ctx)
		eg, _ := errgroup.WithContext(batchCtx)
		eg.SetLimit(concurrencyLimit)

		// Process the current batch of pages
		for _, pageNumber := range currentBatch {
			fullPath := filepath.Join(interactiveOutputRoot, fmt.Sprintf("interactive-%d.png", pageNumber))

			if _, err := os.Stat(fullPath); err == nil {
				// File already exists, add to captured pages
				mutex.Lock()
				capturedPages = append(capturedPages, book.InteractivePageImage{
					PageNumber:   pageNumber,
					OverallOrder: pageNumber,
					Url:          fmt.Sprintf("%s#p=%d", b.Url, pageNumber),
					FullPath:     fullPath,
				})
				mutex.Unlock()

				// If page is even and not the last page, also create a reference for the odd page
				// but don't duplicate the actual file
				if pageNumber > 1 && pageNumber%2 == 0 && pageNumber < len(b.Pages) {
					oddPageNumber := pageNumber + 1

					mutex.Lock()
					capturedPages = append(capturedPages, book.InteractivePageImage{
						PageNumber:   oddPageNumber,
						OverallOrder: oddPageNumber,
						Url:          fmt.Sprintf("%s#p=%d", b.Url, oddPageNumber),
						FullPath:     fullPath, // Use the same file path as the even page
					})
					mutex.Unlock()
				}

				// Update progress counters
				atomic.AddInt32(&completedPages, 1)
				if err := batchBar.Add(1); err != nil {
					fmt.Fprintf(os.Stderr, "Error updating progress bar: %v\n", err)
				}
			} else {
				// File doesn't exist, queue for processing
				pageNum := pageNumber // Create a copy for the closure
				eg.Go(func() error {
					// Page URL is the direct URL to the page in the flipbook viewer
					pageUrl := fmt.Sprintf("%s#p=%d", b.Url, pageNum)

					// Create an isolated context for this particular page
					pageCtx, cancelPage := context.WithCancel(batchCtx)
					defer cancelPage()

					// Add a small delay between starting each browser to reduce race conditions
					time.Sleep(time.Millisecond * 200)

					// Use quiet mode for less log clutter during captures
					result, err := book.CaptureInteractivePageQuiet(pageCtx, pageUrl, interactiveOutputRoot, pageNum, pageNum)
					if err != nil {
						fmt.Fprintf(os.Stderr, "\nError capturing page %d: %v\n", pageNum, err)
						mutex.Lock()
						failedPages = append(failedPages, pageNum)
						mutex.Unlock()
					} else {
						mutex.Lock()
						capturedPages = append(capturedPages, *result)

						// If page is even and not the last page, also create a reference for the odd page
						// but don't duplicate the actual file
						if pageNum > 1 && pageNum%2 == 0 && pageNum < len(b.Pages) {
							oddPageNumber := pageNum + 1

							capturedPages = append(capturedPages, book.InteractivePageImage{
								PageNumber:   oddPageNumber,
								OverallOrder: oddPageNumber,
								Url:          fmt.Sprintf("%s#p=%d", b.Url, oddPageNumber),
								FullPath:     result.FullPath, // Use the same file path as the even page
							})
						}
						mutex.Unlock()
					}

					// Update progress and display estimated time to completion
					atomic.AddInt32(&completedPages, 1)
					if err := batchBar.Add(1); err != nil {
						fmt.Fprintf(os.Stderr, "Error updating progress bar: %v\n", err)
					}

					// Calculate and display estimated time remaining
					elapsed := time.Since(startTime)
					completed := atomic.LoadInt32(&completedPages)
					if completed > 0 {
						pagesPerSecond := float64(completed) / elapsed.Seconds()
						if pagesPerSecond > 0 {
							remaining := float64(totalPages-int(completed)) / pagesPerSecond
							remainingTime := time.Duration(remaining * float64(time.Second))
							fmt.Printf("\rEST remaining: %s, Progress: %d/%d (%.1f%%)                    ",
								formatDuration(remainingTime),
								completed,
								totalPages,
								float64(completed)/float64(totalPages)*100)
						}
					}

					return nil
				})
			}
		}

		// Wait for batch to complete
		if err := eg.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "Error in batch %d: %v\n", batchIndex+1, err)
			// Continue to next batch despite errors
		}

		// Close batch context
		batchCancel()

		if err := batchBar.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing batch progress bar: %v\n", err)
		}

		// Force garbage collection between batches
		runtime.GC()

		// Add a pause between batches to let resources be properly cleaned up
		if batchIndex < numBatches-1 {
			fmt.Printf("Pausing between batches for cleanup...\n")
			time.Sleep(time.Second * 2)
		}
	}

	// Report failed pages
	if len(failedPages) > 0 {
		sort.Ints(failedPages)
		fmt.Printf("\nWARNING: Failed to capture %d pages: %v\n", len(failedPages), failedPages)
	}

	// Sort the captured pages
	sort.Slice(capturedPages, func(i, j int) bool {
		return capturedPages[i].OverallOrder < capturedPages[j].OverallOrder
	})

	// If no pages were captured, return an error
	if len(capturedPages) == 0 {
		return nil, fmt.Errorf("failed to capture any pages")
	}

	// Retry failed pages in sequential mode if there are failures
	if len(failedPages) > 0 && len(failedPages) < len(pagesToCapture) {
		fmt.Printf("\nRetrying %d failed pages in sequential mode...\n", len(failedPages))

		retryBar := progressbar.Default(int64(len(failedPages)), "Retrying failed pages")

		for _, pageNum := range failedPages {
			pageUrl := fmt.Sprintf("%s#p=%d", b.Url, pageNum)

			// Give extra time between retries
			time.Sleep(time.Second * 3)

			// Create a fresh context for each retry
			retryCtx, cancelRetry := context.WithCancel(ctx)
			result, err := book.CaptureInteractivePageQuiet(retryCtx, pageUrl, interactiveOutputRoot, pageNum, pageNum)
			cancelRetry()

			if err != nil {
				fmt.Fprintf(os.Stderr, "Still failed to capture page %d on retry: %v\n", pageNum, err)
			} else {
				mutex.Lock()
				capturedPages = append(capturedPages, *result)

				// If page is even and not the last page, also create a reference for the odd page
				// but don't duplicate the actual file
				if pageNum > 1 && pageNum%2 == 0 && pageNum < len(b.Pages) {
					oddPageNumber := pageNum + 1

					capturedPages = append(capturedPages, book.InteractivePageImage{
						PageNumber:   oddPageNumber,
						OverallOrder: oddPageNumber,
						Url:          fmt.Sprintf("%s#p=%d", b.Url, oddPageNumber),
						FullPath:     result.FullPath, // Use the same file path as the even page
					})
				}

				mutex.Unlock()
				fmt.Printf("Successfully captured page %d on retry\n", pageNum)
			}

			if err := retryBar.Add(1); err != nil {
				fmt.Fprintf(os.Stderr, "Error updating retry progress bar: %v\n", err)
			}

			// Force GC after each retry
			runtime.GC()
		}

		// Sort the captured pages again after retries
		sort.Slice(capturedPages, func(i, j int) bool {
			return capturedPages[i].OverallOrder < capturedPages[j].OverallOrder
		})

		if err := retryBar.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing retry progress bar: %v\n", err)
		}
	}

	return capturedPages, nil
}

// formatDuration formats time.Duration to a human-readable string (HH:MM:SS)
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

// downloadPdf2 is a wrapper function that can be called from the terminal UI
func downloadPdf2(ctx context.Context, args *Args) error {
	// Make sure the args struct is properly initialized
	if args.Concurrency <= 0 {
		args.Concurrency = runtime.NumCPU() - 1
		if args.Concurrency <= 0 {
			args.Concurrency = 1
		}
	}

	// Process the book
	b, err := book.Get(args.Url)
	if err != nil {
		return tracerr.Wrap(err)
	}

	// Create the output directory if it doesn't exist
	outputDir, err := filepath.Abs(args.OutputFolder)
	if err != nil {
		return tracerr.Wrap(err)
	}

	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		err = os.MkdirAll(outputDir, os.ModePerm)
		if err != nil {
			return tracerr.Wrap(err)
		}
	}

	// Check if PDF already exists
	sanitizedTitle := sanitizeFilename(b.Title)
	pdfPath := filepath.Join(outputDir, sanitizedTitle+".pdf")
	if _, err := os.Stat(pdfPath); err == nil && !args.Force {
		fmt.Printf("PDF %s already exists. Skipping.\n", pdfPath)
		return nil
	}

	// Get all the images in the book
	images := b.FindAllImages()

	// Optimize: Limit number of images to download if the book has too many
	// Some books have duplicate images or too many unneeded images
	if len(images) > 1000 {
		fmt.Printf("WARNING: Book has %d images. Limiting to first 1000 to avoid excessive downloads.\n", len(images))
		images = images[:1000]
	}

	// Download images with progress tracking
	downloadStartTime := time.Now()
	downloadedImages, err := downloadImages(ctx, args, images)
	if err != nil {
		return tracerr.Wrap(err)
	}

	downloadDuration := time.Since(downloadStartTime)
	fmt.Printf("Images downloaded in %s\n", formatDuration(downloadDuration))

	// If interactive mode is enabled, also capture screenshots
	if args.Interactive {
		captureStartTime := time.Now()
		interactiveImages, err := captureInteractivePages(ctx, args, b)
		if err != nil {
			return tracerr.Wrap(err)
		}

		captureDuration := time.Since(captureStartTime)
		fmt.Printf("Interactive captures completed in %s\n", formatDuration(captureDuration))

		// Generate PDF with interactive screenshots
		if len(interactiveImages) > 0 {
			// Build a PDF from the downloaded images
			pdfStartTime := time.Now()
			err = generateInteractivePDF(downloadedImages, interactiveImages, pdfPath, args.Force)
			if err != nil {
				return tracerr.Wrap(err)
			}

			pdfDuration := time.Since(pdfStartTime)
			fmt.Printf("PDF generation completed in %s\n", formatDuration(pdfDuration))
		} else {
			// If no interactive images were captured, generate a regular PDF
			pdfStartTime := time.Now()
			err = generatePDF(downloadedImages, pdfPath, args.Force)
			if err != nil {
				return tracerr.Wrap(err)
			}

			pdfDuration := time.Since(pdfStartTime)
			fmt.Printf("PDF generation completed in %s\n", formatDuration(pdfDuration))
		}
	} else {
		// Generate a regular PDF
		pdfStartTime := time.Now()
		err = generatePDF(downloadedImages, pdfPath, args.Force)
		if err != nil {
			return tracerr.Wrap(err)
		}

		pdfDuration := time.Since(pdfStartTime)
		fmt.Printf("PDF generation completed in %s\n", formatDuration(pdfDuration))
	}

	totalDuration := time.Since(downloadStartTime)
	fmt.Printf("Total processing time: %s\n", formatDuration(totalDuration))

	return nil
}

// generateInteractivePDF combines regular images with interactive screenshots
func generateInteractivePDF(downloadedImages []book.DownloadedImage, interactiveImages []book.InteractivePageImage, pdfPath string, force bool) error {
	// First check if the PDF already exists and should be overwritten
	if _, err := os.Stat(pdfPath); err == nil && !force {
		return fmt.Errorf("PDF %s already exists. Use -f flag to overwrite", pdfPath)
	}

	// Create a PDF configuration
	pdfConfig := model.NewDefaultConfiguration()

	// Map page numbers to the actual images that should be used
	pageMap := make(map[int]string)

	// First, add all normal images to the map
	for _, img := range downloadedImages {
		pageMap[img.PageNumber] = img.FullPath
	}

	// Then, override with interactive images where available
	for _, intImg := range interactiveImages {
		pageMap[intImg.PageNumber] = intImg.FullPath
	}

	// Sort the page numbers for consistent ordering
	pageNums := make([]int, 0, len(pageMap))
	for num := range pageMap {
		pageNums = append(pageNums, num)
	}
	sort.Ints(pageNums)

	// Create the ordered list of images to include in the PDF
	var images []string
	for _, num := range pageNums {
		images = append(images, pageMap[num])
	}

	// Generate the PDF using the ImportImagesFile function which is compatible with newer pdfcpu versions
	err := pdfcpu_api.ImportImagesFile(images, pdfPath, nil, pdfConfig)
	if err != nil {
		return tracerr.Wrap(err)
	}

	return nil
}

// generatePDF generates a PDF from the downloaded images
func generatePDF(images []book.DownloadedImage, pdfPath string, force bool) error {
	// Check if the PDF already exists
	if _, err := os.Stat(pdfPath); err == nil && !force {
		return fmt.Errorf("PDF %s already exists. Use -f flag to overwrite", pdfPath)
	}

	// Create a PDF configuration
	pdfConfig := model.NewDefaultConfiguration()

	// Create a list of image paths
	imageFiles := make([]string, len(images))
	for i, img := range images {
		imageFiles[i] = img.FullPath
	}

	// Generate the PDF using the ImportImagesFile function
	err := pdfcpu_api.ImportImagesFile(imageFiles, pdfPath, nil, pdfConfig)
	if err != nil {
		return tracerr.Wrap(err)
	}

	return nil
}

// Main function with error handling
func mainWithErrors() error {
	// Parse the command line arguments first
	var args Args

	// Parse arguments
	argP := arg.MustParse(&args)

	// Check if Terminal UI is requested via the flag
	if args.TerminalUI {
		// Launch the Terminal UI
		RunTerminalUI()
		return nil
	}

	// For regular CLI mode, URL is required
	if args.Url == "" {
		argP.WriteHelp(os.Stderr)
		return fmt.Errorf("URL or ID is required")
	}

	// Set default concurrency
	if args.Concurrency <= 0 {
		args.Concurrency = runtime.NumCPU() - 1
		if args.Concurrency <= 0 {
			args.Concurrency = 1
		}
	}

	// Run the download with the provided arguments
	ctx := context.Background()
	return downloadPdf2(ctx, &args)
}

// Main entry point
func main() {
	if err := mainWithErrors(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// extractPageNumber extracts the page number from a filename
func extractPageNumber(filename string) int {
	base := filepath.Base(filename)
	numStr := strings.TrimPrefix(base, "interactive-")
	numStr = strings.TrimSuffix(numStr, ".png")

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0 // Default to 0 if we can't parse
	}
	return num
}

// Helper function to run the terminal UI, called when -t or --termui is specified
func runTerminalUI() {
	// Call the terminal UI implementation from termui.go
	RunTerminalUI()
}

// sanitizeFilename sanitizes a filename to remove invalid characters
func sanitizeFilename(filename string) string {
	invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalidChars {
		filename = strings.ReplaceAll(filename, char, "")
	}
	return filename
}
