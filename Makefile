
# Recursively build Go main binaries in all subdirectories (fixes relative path bug)
# Usage:
#   make        # build all binaries
#   make clean  # remove all binaries

# Cross-platform color support
SHELL := /bin/bash

# Method 1: Using tput (most reliable cross-platform)
ifneq ($(shell which tput 2>/dev/null),)
	HAS_TPUT := true
	GREEN  := $(shell tput setaf 2 2>/dev/null || echo '')
	YELLOW := $(shell tput setaf 3 2>/dev/null || echo '')
	RED    := $(shell tput setaf 1 2>/dev/null || echo '')
	RESET  := $(shell tput sgr0 2>/dev/null || echo '')
else
	HAS_TPUT := false
endif

# Method 2: Force ANSI escape sequences (fallback)
ifeq ($(HAS_TPUT),false)
	GREEN  := \033[32m
	YELLOW := \033[33m
	RED    := \033[31m
	RESET  := \033[0m
endif

# Method 3: Alternative using printf (uncomment to use)
# GREEN  := $(shell printf '\033[32m')
# YELLOW := $(shell printf '\033[33m')
# RED    := $(shell printf '\033[31m')
# RESET  := $(shell printf '\033[0m')

# Method 4: No colors (uncomment to disable colors completely)
# GREEN  :=
# YELLOW :=
# RED    :=
# RESET  :=

# Find all directories under project containing Go main packages (relative to workspace)
GODIRS := $(shell find . -type f -name '*.go' | xargs grep -l '^package main' | xargs -n1 dirname | sort -u)
BINS := $(foreach dir,$(GODIRS),$(notdir $(dir)))

.PHONY: all clean $(BINS) test-colors

BIN_COUNT := $(words $(BINS))

all: $(BINS)

# Test color output
test-colors:
	@printf "Testing color output methods:\n"
	@printf "Method 1 (tput): $(GREEN)Green$(RESET) $(YELLOW)Yellow$(RESET) $(RED)Red$(RESET)\n"
	@printf "Method 2 (ANSI): \033[32mGreen\033[0m \033[33mYellow\033[0m \033[31mRed\033[0m\n"
	@printf "Method 3 (printf): %bGreen%b %bYellow%b %bRed%b\n" '\033[32m' '\033[0m' '\033[33m' '\033[0m' '\033[31m' '\033[0m'

$(BINS):
	@IDX=$$(expr $$(echo "$(BINS)" | tr ' ' '\n' | grep -n "^$@$$" | cut -d: -f1)) ; \
	printf "$(GREEN)[Build $${IDX}/$(BIN_COUNT)]$(RESET) Building $@ from $(filter %/$@,$(GODIRS))\n" ; \
	if go build -v -o bin/$@ $(filter %/$@,$(GODIRS)); then \
		printf "$(GREEN)✔ Success: $@$(RESET)\n" ; \
	else \
		printf "$(RED)✖ Failed: $@$(RESET)\n" ; \
		exit 1 ; \
	fi

clean:
	@printf "$(YELLOW)Cleaning binaries:$(RESET) $(BINS)\n"
	@rm -f $(addprefix bin/,$(BINS))
