
# Recursively build Go main binaries in all subdirectories (fixes relative path bug)
# Usage:
#   make        # build all binaries
#   make clean  # remove all binaries

# Find all directories under project containing Go main packages (relative to workspace)
GODIRS := $(shell find . -type f -name '*.go' | xargs grep -l '^package main' | xargs -n1 dirname | sort -u)
BINS := $(foreach dir,$(GODIRS),$(notdir $(dir)))

.PHONY: all clean $(BINS)

all: $(BINS)

$(BINS):
	@echo "Building $@ from $(CURDIR)/$@"
	@go build -v -o bin/$@ $(filter %/$@,$(GODIRS))

clean:
	@echo "Cleaning binaries: $(BINS)"
	@rm -f $(BINS)
