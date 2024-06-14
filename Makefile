2GOARRAY := 2goarray
PACKAGE := main
PNG_FILES := icon_blue.png

GO_FILES := $(PNG_FILES:.png=.go)

# Default target
all: $(GO_FILES)

# Rule to convert PNG to Go using 2goarray
%.go: %.png
	cat $< | $(2GOARRAY) $(basename $<) $(PACKAGE) > $@

# Clean generated Go files
clean:
	rm -f $(GO_FILES)

.PHONY: all clean

