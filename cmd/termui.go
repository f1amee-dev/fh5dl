package main

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
)

// app settings represents user configurable settings
type AppSettings struct {
	Concurrency  int    // number of concurrent downloads
	BatchSize    int    // batch size for interactive captures
	OutputFolder string // default output folder
	SkipExisting bool   // skip existing files
}

// default settings
var defaultSettings = AppSettings{
	Concurrency:  runtime.NumCPU() - 1,
	BatchSize:    8,
	OutputFolder: "output",
	SkipExisting: true,
}

// model represents the state of our application
type uiModel struct {
	choices        []string
	cursor         int
	selected       bool
	downloadType   string
	url            string
	interactive    bool
	booksDirectory string
	settings       AppSettings
	settingsMode   bool
	settingCursor  int
	settingOptions []string
	editingValue   bool
	editValue      string
	confirmation   string // for yes/no confirmation
}

// initial model setup
func initialModel() uiModel {
	return uiModel{
		choices: []string{
			"Single File Download (Non-interactive)",
			"Single File Download (Interactive)",
			"Batch Download from Books Folder",
			"Settings",
			"Quit",
		},
		booksDirectory: "books",
		settings:       defaultSettings,
		settingOptions: []string{
			"Concurrency",
			"Batch Size",
			"Output Folder",
			"Skip Existing Files",
			"Back to Main Menu",
		},
	}
}

// define some styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			PaddingLeft(2).
			PaddingRight(2).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A49FA5"))

	settingLabelStyle = lipgloss.NewStyle().
				Width(20).
				Foreground(lipgloss.Color("#7D56F4"))

	settingValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205"))
)

// init initializes the model
func (m uiModel) Init() tea.Cmd {
	return nil
}

// update handles user interactions
func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// handle key presses
		switch msg.String() {
		case "ctrl+c", "q":
			if !m.selected && !m.settingsMode {
				return m, tea.Quit
			} else if m.settingsMode {
				// exit settings mode
				m.settingsMode = false
				m.editingValue = false
				return m, nil
			} else {
				// go back to the menu
				m.selected = false
				m.confirmation = "" // reset confirmation
				return m, nil
			}
		case "up", "k":
			if !m.selected && !m.settingsMode && m.cursor > 0 {
				m.cursor--
			} else if m.settingsMode && !m.editingValue && m.settingCursor > 0 {
				m.settingCursor--
			}
		case "down", "j":
			if !m.selected && !m.settingsMode && m.cursor < len(m.choices)-1 {
				m.cursor++
			} else if m.settingsMode && !m.editingValue && m.settingCursor < len(m.settingOptions)-1 {
				m.settingCursor++
			}
		case "enter":
			if m.settingsMode {
				if m.editingValue {
					// save the edited value
					switch m.settingCursor {
					case 0: // concurrency
						val, err := strconv.Atoi(m.editValue)
						if err == nil && val > 0 {
							m.settings.Concurrency = val
						}
					case 1: // batch size
						val, err := strconv.Atoi(m.editValue)
						if err == nil && val > 0 {
							m.settings.BatchSize = val
						}
					case 2: // output folder
						if m.editValue != "" {
							m.settings.OutputFolder = m.editValue
						}
					case 3: // skip existing
						m.settings.SkipExisting = !m.settings.SkipExisting
					}
					m.editingValue = false
				} else if m.settingCursor == len(m.settingOptions)-1 {
					// back to main menu
					m.settingsMode = false
				} else {
					// start editing the selected setting
					switch m.settingCursor {
					case 0: // concurrency
						m.editValue = fmt.Sprintf("%d", m.settings.Concurrency)
						m.editingValue = true
					case 1: // batch size
						m.editValue = fmt.Sprintf("%d", m.settings.BatchSize)
						m.editingValue = true
					case 2: // output folder
						m.editValue = m.settings.OutputFolder
						m.editingValue = true
					case 3: // skip existing files (toggle)
						m.settings.SkipExisting = !m.settings.SkipExisting
					}
				}
			} else if !m.selected {
				// process the selection
				switch m.cursor {
				case 0: // single file download (non-interactive)
					m.downloadType = "single"
					m.interactive = false
					m.selected = true
				case 1: // single file download (interactive)
					m.downloadType = "single"
					m.interactive = true
					m.selected = true
				case 2: // batch download from books folder
					m.downloadType = "batch"
					m.selected = true
					m.confirmation = "" // initialize confirmation
				case 3: // settings
					m.settingsMode = true
					m.settingCursor = 0
					return m, nil
				case 4: // quit
					return m, tea.Quit
				}
			} else if m.downloadType == "single" {
				// process the URL input
				if m.url != "" {
					return m, tea.Quit
				}
			}
		case "esc":
			if m.settingsMode && m.editingValue {
				m.editingValue = false
			} else if m.settingsMode {
				m.settingsMode = false
			} else if m.selected {
				m.selected = false
			}
		}
	}

	// If a key was pressed, we're typing a URL or editing a setting value or confirmation
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter", "up", "down", "ctrl+c", "esc":
			// Handled above
		case "y", "Y":
			if m.selected && m.downloadType == "batch" {
				m.confirmation = "y"
				return m, tea.Quit // Exit and start the download
			}
		case "n", "N":
			if m.selected && m.downloadType == "batch" {
				// Handle "no" answer for batch confirmation
				m.confirmation = "" // Reset confirmation
				m.selected = false  // Go back to main menu
			} else {
				// Treat it as a normal character input
				if keyMsg.Type == tea.KeyRunes {
					if m.selected && m.downloadType == "single" {
						m.url += string(keyMsg.Runes)
					} else if m.settingsMode && m.editingValue {
						m.editValue += string(keyMsg.Runes)
					}
				}
			}
		case "backspace":
			if m.selected && m.downloadType == "single" && len(m.url) > 0 {
				m.url = m.url[:len(m.url)-1]
			} else if m.settingsMode && m.editingValue && len(m.editValue) > 0 {
				m.editValue = m.editValue[:len(m.editValue)-1]
			}
		default:
			// Add the typed character to the URL or setting value
			if keyMsg.Type == tea.KeyRunes {
				if m.selected && m.downloadType == "single" {
					m.url += string(keyMsg.Runes)
				} else if m.settingsMode && m.editingValue {
					m.editValue += string(keyMsg.Runes)
				}
			}
		}
	}

	return m, nil
}

// View renders the UI
func (m uiModel) View() string {
	if m.settingsMode {
		return m.settingsView()
	}

	if !m.selected {
		// Main menu
		s := titleStyle.Render("FlipHTML5 Downloader") + "\n\n"
		s += "Select an option:\n\n"

		for i, choice := range m.choices {
			cursor := " "
			if m.cursor == i {
				cursor = ">"
				choice = selectedStyle.Render(choice)
			}
			s += fmt.Sprintf("%s %s\n", cursor, choice)
		}

		s += "\n" + infoStyle.Render("Press q to quit, arrow keys to navigate, enter to select")
		return s
	}

	// Handle different selected options
	switch m.downloadType {
	case "single":
		s := titleStyle.Render("FlipHTML5 Downloader - Single File") + "\n\n"
		interactiveStatus := "Non-Interactive"
		if m.interactive {
			interactiveStatus = "Interactive"
		}
		s += fmt.Sprintf("Mode: %s\n\n", interactiveStatus)
		s += "Enter the URL (or ID) of the document to download:\n"
		s += fmt.Sprintf("> %s\n", m.url)
		s += "\nPress Enter to download, Esc to go back\n"
		return s
	case "batch":
		s := titleStyle.Render("FlipHTML5 Downloader - Batch Mode") + "\n\n"
		s += fmt.Sprintf("Starting batch download from: %s\n", m.booksDirectory)
		s += fmt.Sprintf("Using concurrency: %d\n", m.settings.Concurrency)
		s += fmt.Sprintf("Output folder: %s\n\n", m.settings.OutputFolder)
		s += selectedStyle.Render("Are you sure you want to start the batch download? (y/n)")
		return s
	default:
		return "Unknown option"
	}
}

// settingsView renders the settings menu
func (m uiModel) settingsView() string {
	s := titleStyle.Render("FlipHTML5 Downloader - Settings") + "\n\n"

	for i, option := range m.settingOptions {
		cursor := " "
		if m.settingCursor == i {
			cursor = ">"
			option = selectedStyle.Render(option)
		}

		if i < len(m.settingOptions)-1 { // Not the "Back" option
			s += fmt.Sprintf("%s %s", cursor, settingLabelStyle.Render(option))

			// Show current value or editing field
			if m.editingValue && m.settingCursor == i {
				// Show editing field
				s += fmt.Sprintf(": %s_\n", m.editValue)
			} else {
				// Show current value
				switch i {
				case 0: // Concurrency
					s += fmt.Sprintf(": %s\n", settingValueStyle.Render(fmt.Sprintf("%d", m.settings.Concurrency)))
				case 1: // Batch Size
					s += fmt.Sprintf(": %s\n", settingValueStyle.Render(fmt.Sprintf("%d", m.settings.BatchSize)))
				case 2: // Output Folder
					s += fmt.Sprintf(": %s\n", settingValueStyle.Render(m.settings.OutputFolder))
				case 3: // Skip Existing
					value := "No"
					if m.settings.SkipExisting {
						value = "Yes"
					}
					s += fmt.Sprintf(": %s\n", settingValueStyle.Render(value))
				}
			}
		} else {
			// The "Back" option
			s += fmt.Sprintf("%s %s\n", cursor, option)
		}
	}

	s += "\n" + infoStyle.Render("Press Enter to edit a setting, Esc to go back")
	return s
}

// RunTerminalUI starts the terminal UI
func RunTerminalUI() {
	// Create the Bubble Tea program
	p := tea.NewProgram(initialModel())
	m, err := p.Run()
	if err != nil {
		fmt.Printf("Error running UI: %v\n", err)
		os.Exit(1)
	}

	// Get the final model state
	finalModel := m.(uiModel)

	// Process the selected option
	if finalModel.selected {
		switch finalModel.downloadType {
		case "single":
			// Process single file download
			url := finalModel.url
			if finalModel.interactive {
				// Ensure the URL doesn't already have -i suffix
				if !strings.HasSuffix(url, "-i") {
					url += "-i"
				}
			}
			downloadSingleFile(url, finalModel.settings)
		case "batch":
			// Process batch download only if confirmed with "y"
			if finalModel.confirmation == "y" {
				downloadBatch(finalModel.booksDirectory, finalModel.settings)
			}
		}
	}
}

// downloadSingleFile handles downloading a single file
func downloadSingleFile(url string, settings AppSettings) {
	interactive := false

	// Check if URL ends with -i and remove it for processing
	if strings.HasSuffix(url, "-i") {
		interactive = true
		url = strings.TrimSuffix(url, "-i")
	}

	// Set up arguments for the main download function
	args := Args{
		Url:          url,
		OutputFolder: settings.OutputFolder,
		Force:        !settings.SkipExisting,
		Interactive:  interactive,
		Concurrency:  settings.Concurrency,
		BatchSize:    settings.BatchSize,
	}

	// Create a colorized progress indicator
	success := color.New(color.FgGreen).SprintFunc()
	info := color.New(color.FgCyan).SprintFunc()

	fmt.Printf("%s Downloading %s\n", info("INFO:"), url)
	if interactive {
		fmt.Printf("%s Interactive mode enabled\n", info("INFO:"))
	}

	// Run the download
	start := time.Now()
	err := downloadPdf2(context.Background(), &args)
	if err != nil {
		color.Red("ERROR: %v", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	fmt.Printf("%s Download completed in %s\n", success("SUCCESS:"), duration)
}

// downloadBatch handles downloading all files in the books directory
func downloadBatch(booksDir string, settings AppSettings) {
	// Check if books directory exists
	if _, err := os.Stat(booksDir); os.IsNotExist(err) {
		color.Red("ERROR: Books directory '%s' not found", booksDir)
		os.Exit(1)
	}

	// Get list of files
	files, err := ioutil.ReadDir(booksDir)
	if err != nil {
		color.Red("ERROR: Failed to read books directory: %v", err)
		os.Exit(1)
	}

	// Filter for .txt files
	var txtFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".txt") {
			txtFiles = append(txtFiles, file.Name())
		}
	}

	if len(txtFiles) == 0 {
		color.Red("ERROR: No book files found in %s", booksDir)
		os.Exit(1)
	}

	info := color.New(color.FgCyan).SprintFunc()
	success := color.New(color.FgGreen).SprintFunc()
	warning := color.New(color.FgYellow).SprintFunc()

	// Display batch statistics
	fmt.Printf("%s Found %d book files to download\n", info("INFO:"), len(txtFiles))
	fmt.Printf("%s Using concurrency: %d\n", info("INFO:"), settings.Concurrency)
	fmt.Printf("%s Output folder: %s\n", info("INFO:"), settings.OutputFolder)
	if settings.BatchSize > 0 {
		fmt.Printf("%s Batch size for interactive captures: %d\n", info("INFO:"), settings.BatchSize)
	}

	// Create output folder if it doesn't exist
	if _, err := os.Stat(settings.OutputFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(settings.OutputFolder, 0755); err != nil {
			color.Red("ERROR: Failed to create output folder: %v", err)
			os.Exit(1)
		}
	}

	// Process each file
	failedDownloads := 0
	successfulDownloads := 0
	skippedDownloads := 0

	// Track start time for ETA calculation
	startTime := time.Now()

	// Create a map to track downloaded URLs to avoid duplicates
	downloadedURLs := make(map[string]bool)

	for i, fileName := range txtFiles {
		// Calculate ETA
		if i > 0 {
			elapsed := time.Since(startTime)
			timePerBook := elapsed / time.Duration(i)
			eta := timePerBook * time.Duration(len(txtFiles)-i)
			fmt.Printf("%s ETA: %s remaining for batch completion (avg: %s per book)\n",
				info("TIME:"), formatDuration(eta), formatDuration(timePerBook))
		}

		// Open the file
		filePath := filepath.Join(booksDir, fileName)
		file, err := os.Open(filePath)
		if err != nil {
			color.Red("ERROR: Cannot open file %s: %v", fileName, err)
			failedDownloads++
			continue
		}

		// Read the URL from the file
		scanner := bufio.NewScanner(file)
		if !scanner.Scan() {
			file.Close()
			color.Red("ERROR: Empty file or failed to read %s", fileName)
			failedDownloads++
			continue
		}

		url := strings.TrimSpace(scanner.Text())
		file.Close()

		// Skip empty URLs
		if url == "" {
			color.Red("ERROR: Empty URL in file %s", fileName)
			failedDownloads++
			continue
		}

		// Check if we've already downloaded this URL
		if _, exists := downloadedURLs[url]; exists {
			fmt.Printf("\n%s [%d/%d] Skipping %s (Already downloaded this URL)\n",
				warning("SKIP:"), i+1, len(txtFiles), fileName)
			skippedDownloads++
			continue
		}

		// Check for interactive mode flag
		interactive := false
		if strings.HasSuffix(url, "-i") {
			interactive = true
			url = strings.TrimSuffix(url, "-i")
		}

		// Extract book ID to use as file name
		bookID, err := extractBookID(url)
		if err != nil {
			// Generate a safe filename from the original name
			bookID = generateSafeID(fileName)
		}

		// Create a dedicated folder for this book
		bookOutputFolder := filepath.Join(settings.OutputFolder, bookID)
		if _, err := os.Stat(bookOutputFolder); os.IsNotExist(err) {
			if err := os.MkdirAll(bookOutputFolder, 0755); err != nil {
				color.Red("ERROR: Failed to create book output folder: %v", err)
				failedDownloads++
				continue
			}
		}

		// Check if the PDF already exists
		pdfPath := filepath.Join(bookOutputFolder, bookID+".pdf")
		if _, err := os.Stat(pdfPath); err == nil && settings.SkipExisting {
			fmt.Printf("\n%s [%d/%d] Skipping %s (PDF already exists)\n",
				warning("SKIP:"), i+1, len(txtFiles), fileName)
			skippedDownloads++
			continue
		}

		// Print progress
		fmt.Printf("\n%s [%d/%d] Downloading: %s\n", info("INFO:"), i+1, len(txtFiles), fileName)
		if interactive {
			fmt.Printf("%s Interactive mode enabled\n", info("INFO:"))
		}
		fmt.Printf("%s URL: %s\n", info("INFO:"), url)
		fmt.Printf("%s Output: %s\n", info("INFO:"), bookOutputFolder)

		// Set up arguments for the download
		args := Args{
			Url:               url,
			OutputFolder:      bookOutputFolder,
			ImageOutputFolder: filepath.Join(bookOutputFolder, "images"),
			Force:             !settings.SkipExisting,
			Interactive:       interactive,
			Concurrency:       settings.Concurrency,
			BatchSize:         settings.BatchSize,
		}

		// Make sure to use unique temp dirs for each download
		os.Setenv("TMPDIR", bookOutputFolder)

		// Run the download with a timeout to prevent hanging
		downloadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		bookStartTime := time.Now()
		err = downloadPdf2(downloadCtx, &args)
		bookDuration := time.Since(bookStartTime)
		cancel()

		if err != nil {
			color.Red("ERROR: Failed to download %s: %v", fileName, err)
			failedDownloads++
		} else {
			successfulDownloads++
			downloadedURLs[url] = true // Mark as downloaded
			fmt.Printf("%s Download completed for %s in %s\n",
				success("SUCCESS:"), fileName, formatDuration(bookDuration))
		}

		// Brief pause between downloads to clean up resources
		fmt.Printf("%s Cleaning up resources before next download...\n", info("INFO:"))
		time.Sleep(2 * time.Second)
		runtime.GC() // Force garbage collection between books
	}

	// Show final statistics
	totalTime := time.Since(startTime)
	fmt.Printf("\n%s Batch download completed in %s\n", success("SUCCESS:"), formatDuration(totalTime))
	fmt.Printf("Total files: %d\n", len(txtFiles))
	fmt.Printf("Successful: %d\n", successfulDownloads)
	fmt.Printf("Skipped: %d\n", skippedDownloads)
	fmt.Printf("Failed: %d\n", failedDownloads)
}

// generateSafeID creates a safe ID from a filename
func generateSafeID(fileName string) string {
	// Remove .txt extension
	name := strings.TrimSuffix(fileName, ".txt")

	// Replace problematic characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	safeName := re.ReplaceAllString(name, "_")

	// Truncate if too long (max 64 chars)
	if len(safeName) > 64 {
		safeName = safeName[:64]
	}

	return safeName
}

// extractBookID extracts the book ID from a FlipHTML5 URL
func extractBookID(url string) (string, error) {
	// Extract the book ID from URL patterns like:
	// https://online.fliphtml5.com/kzpyj/cxnu/
	parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid URL format")
	}

	// The last two parts should be the book ID components
	last := len(parts) - 1
	secondLast := len(parts) - 2
	if last >= 0 && secondLast >= 0 {
		return parts[secondLast] + "_" + parts[last], nil
	}

	return "", fmt.Errorf("could not extract book ID")
}

// formatDuration is imported from main.go
