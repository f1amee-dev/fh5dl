# new features in this fork

this document outlines the main enhancements that have been added to the original fh5dl project.

## interactive terminal ui

a sleek terminal ui has been added that allows users to:
- navigate through options with arrow keys
- configure settings interactively
- process batch downloads
- toggle interactive mode

## interactive element capture

added capability to reveal and capture interactive elements in fliphtml5 publications:
- captures hidden text elements by adjusting opacity
- clicks on interactive triggers to reveal content
- generates a more complete representation of the original publication

## batch processing

improved batch processing capabilities:
- process multiple books from a directory
- customize output paths for each book
- skip existing files to resume interrupted downloads

## memory optimization

the codebase has been optimized for handling very large publications:
- processes images in batches to reduce memory footprint
- strategic garbage collection between processing batches
- parallel downloading with configurable concurrency

## improved progress tracking

enhanced progress reporting:
- download speed metrics (images per second)
- estimated time to completion
- visual progress bars
- batch progress indicators

## error handling improvements

more robust error handling throughout the application:
- better retry logic for transient network issues
- more informative error messages
- graceful handling of unexpected page layouts

## configuration options

expanded configuration options:
- customizable concurrency settings
- configurable batch sizes
- image output folder control
- option to force overwrite existing files

## efficiency improvements

overall efficiency enhancements:
- improved image handling
- better resource management 
- optimized chromium flags for interactive capture
- intelligent file existence checking before downloads 