2GOARRAY := 2goarray
PACKAGE := main
PNG_FILES := icon_blue.png icon_red.png icon_green.png

GO_FILES := $(PNG_FILES:.png=.go)

# Run the Go program
run:
	go run *.go

# Default target
icons: $(GO_FILES)

# Rule to convert PNG to Go using 2goarray
%.go: %.png
	cat $< | $(2GOARRAY) $(basename $<) $(PACKAGE) > $@

# Clean generated Go files
clean:
	rm -f $(GO_FILES)

install:
	go install ./...


.PHONY: all clean run

