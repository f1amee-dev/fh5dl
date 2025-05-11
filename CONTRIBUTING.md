# Contributing to FH5DL

we appreciate your interest in contributing to fh5dl! this document provides guidelines and instructions for contributing to this project.

## code of conduct

please be respectful and considerate of others when contributing to this project. we aim to foster an inclusive and welcoming community.

## how to contribute

### reporting bugs

if you find a bug, please create an issue with the following information:

- a clear, descriptive title
- steps to reproduce the issue
- expected behavior vs actual behavior
- any relevant error messages or screenshots
- your environment (os, go version, etc.)

### suggesting features

we welcome feature suggestions! when submitting a feature request, please:

- check if the feature has already been requested
- provide a clear description of the feature and why it would be valuable
- consider how the feature fits into the existing project architecture

### submitting changes

1. fork the repository
2. create a new branch for your changes
3. make your changes, following our code style
4. add tests for your changes if applicable
5. run the existing tests to ensure they still pass
6. commit your changes with a clear commit message
7. submit a pull request

## development setup

to set up your development environment:

```bash
# clone your fork
git clone https://github.com/YOUR_USERNAME/fh5dl.git
cd fh5dl

# add the original repository as a remote
git remote add upstream https://github.com/ygunayer/fh5dl.git

# build the project
go build -o fh5dl ./cmd/main.go

# run tests
go test ./...
```

## code style

please follow these guidelines when contributing code:

- use gofmt to format your code
- follow standard go conventions (see [effective go](https://golang.org/doc/effective_go.html))
- add comments where necessary
- write clear commit messages
- include tests for new functionality

## pull request process

when submitting a pull request:

1. ensure all tests pass
2. update the documentation if necessary
3. describe what your pr does and why it should be included
4. be open to feedback and be prepared to make changes if requested
5. make sure your code follows our style guidelines

thank you for contributing to fh5dl! 