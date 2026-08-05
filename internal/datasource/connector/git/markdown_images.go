package gitconnector

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxGitImagesPerDocument = 30
	maxGitImageBytes        = 10 * 1024 * 1024
	maxGitImageSourceBytes  = 2048
)

type markdownImageTarget struct {
	start int
	end   int
	ref   string
}

type selectedMarkdownImage struct {
	target     markdownImageTarget
	candidates []string
}

func isMarkdownPath(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func imageDependenciesChanged(previous, currentFiles map[string]string) bool {
	for filePath, previousBlob := range previous {
		if currentFiles[filePath] != previousBlob {
			return true
		}
	}
	return false
}

func (c *Connector) embedMarkdownImages(
	ctx context.Context,
	state *repositoryState,
	documentPath string,
	content []byte,
	maxFileBytes int64,
) ([]byte, map[string]string, error) {
	markdown := string(content)
	targets := scanMarkdownImageTargets(markdown)
	dependencies := make(map[string]string)
	dataURIs := make(map[string]string)
	embedded := 0
	attempted := 0

	selected := make([]selectedMarkdownImage, 0, maxGitImagesPerDocument)
	for _, target := range targets {
		candidates := gitImageCandidates(documentPath, target.ref)
		if len(candidates) == 0 {
			continue
		}
		if len(selected) >= maxGitImagesPerDocument {
			break
		}
		for _, candidate := range candidates {
			dependencies[candidate] = state.files[candidate]
		}
		selected = append(selected, selectedMarkdownImage{target: target, candidates: candidates})
	}

	// Reverse replacement keeps byte offsets valid while preserving alt text and optional titles.
	for index := len(selected) - 1; index >= 0; index-- {
		target := selected[index].target
		candidates := selected[index].candidates
		if embedded >= maxGitImagesPerDocument {
			continue
		}
		imagePath := firstExistingGitPath(candidates, state.files)
		if imagePath == "" || !validGitImageSourcePath(imagePath) {
			continue
		}
		dataURI, cached := dataURIs[imagePath]
		if !cached {
			if attempted >= maxGitImagesPerDocument {
				continue
			}
			attempted++
			// Cache rejection too, so repeated invalid references cannot repeat Git I/O.
			dataURIs[imagePath] = ""
			blobSize, err := c.gitBlobSize(ctx, state, state.files[imagePath])
			if err != nil {
				return nil, nil, err
			}
			if blobSize <= 0 || blobSize > maxGitImageBytes {
				continue
			}
			imageData, err := c.runner.Run(
				ctx,
				state.checkout.env,
				"-C", state.checkout.dir,
				"show", "HEAD:"+imagePath,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("read Git image %s: %w", imagePath, err)
			}
			if int64(len(imageData)) != blobSize {
				return nil, nil, fmt.Errorf("Git image size changed while reading: %s", imagePath)
			}
			if len(imageData) == 0 {
				continue
			}
			mimeType := http.DetectContentType(imageData)
			if !strings.HasPrefix(mimeType, "image/") {
				continue
			}
			dataURI = gitImageDataURI(mimeType, imagePath, imageData)
			if dataURI == "" {
				continue
			}
			dataURIs[imagePath] = dataURI
		}
		if dataURI == "" {
			continue
		}
		if len(markdown)-target.end+target.start+len(dataURI) > int(maxFileBytes) {
			continue
		}
		markdown = markdown[:target.start] + dataURI + markdown[target.end:]
		embedded++
	}
	return []byte(markdown), dependencies, nil
}

func (c *Connector) gitBlobSize(
	ctx context.Context,
	state *repositoryState,
	blob string,
) (int64, error) {
	raw, err := c.runner.Run(
		ctx,
		state.checkout.env,
		"-C", state.checkout.dir,
		"cat-file", "-s", blob,
	)
	if err != nil {
		return 0, fmt.Errorf("read Git image size: %w", err)
	}
	return parseGitBlobSize(raw)
}

func parseGitBlobSize(raw []byte) (int64, error) {
	// The command runner combines stdout and stderr. Git auto-gc can therefore
	// prepend housekeeping notices before the numeric cat-file result.
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		value := strings.TrimSpace(lines[index])
		if value == "" || strings.IndexFunc(value, func(character rune) bool {
			return character < '0' || character > '9'
		}) >= 0 {
			continue
		}
		size, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid Git image size: %w", err)
		}
		return size, nil
	}
	return 0, fmt.Errorf("invalid Git image size: missing numeric output")
}

func gitImageDataURI(mimeType, sourcePath string, imageData []byte) string {
	if !validGitImageSourcePath(sourcePath) {
		return ""
	}
	sourceMarker := base64.RawURLEncoding.EncodeToString([]byte(sourcePath))
	return "data:" + mimeType + ";weknora-source=" + sourceMarker + ";base64," +
		base64.StdEncoding.EncodeToString(imageData)
}

func validGitImageSourcePath(sourcePath string) bool {
	if sourcePath == "" || len(sourcePath) > maxGitImageSourceBytes ||
		!utf8.ValidString(sourcePath) {
		return false
	}
	for _, character := range sourcePath {
		if unicode.IsControl(character) {
			return false
		}
	}
	parsed, err := url.Parse(sourcePath)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return false
	}
	normalized, err := normalizeRepoPath(sourcePath)
	return err == nil && normalized == sourcePath
}

func firstExistingGitPath(candidates []string, files map[string]string) string {
	for _, candidate := range candidates {
		if _, exists := files[candidate]; exists {
			return candidate
		}
	}
	return ""
}

func gitImageCandidates(documentPath, rawRef string) []string {
	ref := strings.TrimSpace(strings.ReplaceAll(rawRef, `\ `, " "))
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "//") {
		return nil
	}
	parsed, err := url.Parse(ref)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return nil
	}
	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil || decoded == "" {
		return nil
	}
	switch strings.ToLower(path.Ext(decoded)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
	default:
		return nil
	}

	values := make([]string, 0, 2)
	add := func(candidate string) {
		cleaned, normalizeErr := normalizeRepoPath(candidate)
		if normalizeErr != nil || cleaned == "." {
			return
		}
		for _, existing := range values {
			if existing == cleaned {
				return
			}
		}
		values = append(values, cleaned)
	}
	if strings.HasPrefix(decoded, "/") {
		add(strings.TrimPrefix(decoded, "/"))
		return values
	}
	add(path.Join(path.Dir(documentPath), decoded))
	// Some documentation frameworks resolve asset paths from the repository root.
	add(decoded)
	return values
}

func scanMarkdownImageTargets(markdown string) []markdownImageTarget {
	targets := make([]markdownImageTarget, 0)
	ignored := ignoredMarkdownOffsets(markdown)
	for index := 0; index+1 < len(markdown); index++ {
		if ignored[index] || markdown[index] != '!' || markdown[index+1] != '[' ||
			escapedAt(markdown, index) {
			continue
		}
		altEnd := findImageAltEnd(markdown, index+2)
		if altEnd < 0 {
			continue
		}
		targetStart := altEnd + 2
		targetEnd, ok := findImageTargetEnd(markdown, targetStart)
		if !ok {
			index = altEnd
			continue
		}
		rawStart, rawEnd, ref, ok := splitImageDestination(markdown[targetStart:targetEnd])
		if ok {
			targets = append(targets, markdownImageTarget{
				start: targetStart + rawStart,
				end:   targetStart + rawEnd,
				ref:   ref,
			})
		}
		index = targetEnd
	}
	return targets
}

func ignoredMarkdownOffsets(markdown string) []bool {
	ignored := make([]bool, len(markdown))
	markFencedAndIndentedCode(markdown, ignored)
	markInlineCode(markdown, ignored)
	markHTMLComments(markdown, ignored)
	return ignored
}

func markFencedAndIndentedCode(markdown string, ignored []bool) {
	inFence := false
	var fenceCharacter byte
	fenceLength := 0
	for lineStart := 0; lineStart < len(markdown); {
		lineEnd := strings.IndexByte(markdown[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(markdown)
		} else {
			lineEnd += lineStart + 1
		}
		contentEnd := lineEnd
		if contentEnd > lineStart && markdown[contentEnd-1] == '\n' {
			contentEnd--
		}
		line := markdown[lineStart:contentEnd]
		rest, indent := stripMarkdownContainerPrefix(line)
		runCharacter, runLength := markdownFenceRun(rest)
		isFence := indent <= 3 && runLength >= 3
		if inFence {
			markIgnored(ignored, lineStart, lineEnd)
			if isFence && runCharacter == fenceCharacter && runLength >= fenceLength &&
				strings.TrimSpace(rest[runLength:]) == "" {
				inFence = false
			}
		} else if isFence {
			markIgnored(ignored, lineStart, lineEnd)
			inFence = true
			fenceCharacter = runCharacter
			fenceLength = runLength
		} else if (strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")) &&
			!isListItemImageContinuation(markdown, lineStart, line) {
			markIgnored(ignored, lineStart, lineEnd)
		}
		lineStart = lineEnd
	}
}

func isListItemImageContinuation(markdown string, lineStart int, line string) bool {
	content := ""
	switch {
	case strings.HasPrefix(line, "\t"):
		content = line[1:]
	case strings.HasPrefix(line, "    "):
		content = line[4:]
	}
	// Only exempt one indentation level that contains an image below a list
	// item. Deeper indentation remains an indented code block.
	if !strings.HasPrefix(content, "![") || strings.HasPrefix(content, "\t") ||
		strings.HasPrefix(content, "    ") || lineStart == 0 {
		return false
	}

	// A blank line is valid inside a loose Markdown list item, so walk back to
	// the nearest non-blank line before deciding whether this is list content.
	for cursor := lineStart; cursor > 0; {
		previousEnd := cursor - 1
		if previousEnd > 0 && markdown[previousEnd-1] == '\r' {
			previousEnd--
		}
		previousStart := strings.LastIndexByte(markdown[:previousEnd], '\n') + 1
		previousLine := markdown[previousStart:previousEnd]
		if strings.TrimSpace(previousLine) == "" {
			cursor = previousStart
			continue
		}

		indent := 0
		for indent < len(previousLine) && indent < 3 && previousLine[indent] == ' ' {
			indent++
		}
		return markdownListMarkerLength(previousLine[indent:]) > 0
	}
	return false
}

func stripMarkdownContainerPrefix(line string) (string, int) {
	offset := 0
	totalIndent := 0
	for {
		spaces := 0
		for offset < len(line) && spaces < 4 && line[offset] == ' ' {
			offset++
			spaces++
			totalIndent++
		}
		if totalIndent <= 3 && offset < len(line) && line[offset] == '>' {
			offset++
			if offset < len(line) && line[offset] == ' ' {
				offset++
			}
			totalIndent = 0
			continue
		}
		markerLength := 0
		if totalIndent <= 3 {
			markerLength = markdownListMarkerLength(line[offset:])
		}
		if markerLength > 0 {
			offset += markerLength
			for offset < len(line) && line[offset] == ' ' {
				offset++
			}
			totalIndent = 0
			continue
		}
		return line[offset:], totalIndent
	}
}

func markdownListMarkerLength(line string) int {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '+' || line[0] == '*') &&
		(line[1] == ' ' || line[1] == '\t') {
		return 1
	}
	index := 0
	for index < len(line) && index < 9 && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	if index == 0 || index+1 >= len(line) || (line[index] != '.' && line[index] != ')') ||
		(line[index+1] != ' ' && line[index+1] != '\t') {
		return 0
	}
	return index + 1
}

func markdownFenceRun(line string) (byte, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}
	length := 1
	for length < len(line) && line[length] == line[0] {
		length++
	}
	return line[0], length
}

func markHTMLComments(markdown string, ignored []bool) {
	for offset := 0; offset < len(markdown); {
		start := strings.Index(markdown[offset:], "<!--")
		if start < 0 {
			return
		}
		start += offset
		if ignored[start] {
			offset = start + 4
			continue
		}
		end := strings.Index(markdown[start+4:], "-->")
		if end < 0 {
			end = len(markdown)
		} else {
			end += start + 7
		}
		markIgnored(ignored, start, end)
		offset = end
	}
}

func markInlineCode(markdown string, ignored []bool) {
	for index := 0; index < len(markdown); {
		if ignored[index] || markdown[index] != '`' {
			index++
			continue
		}
		runLength := 1
		for index+runLength < len(markdown) && markdown[index+runLength] == '`' {
			runLength++
		}
		end := findMatchingBacktickRun(markdown, ignored, index+runLength, runLength)
		if end < 0 {
			index += runLength
			continue
		}
		markIgnored(ignored, index, end)
		index = end
	}
}

func findMatchingBacktickRun(markdown string, ignored []bool, start, runLength int) int {
	for index := start; index < len(markdown); index++ {
		if ignored[index] || markdown[index] != '`' {
			continue
		}
		length := 1
		for index+length < len(markdown) && markdown[index+length] == '`' {
			length++
		}
		if length == runLength {
			return index + length
		}
		index += length - 1
	}
	return -1
}

func markIgnored(ignored []bool, start, end int) {
	if end > len(ignored) {
		end = len(ignored)
	}
	for index := start; index < end; index++ {
		ignored[index] = true
	}
}

func findImageAltEnd(markdown string, start int) int {
	for index := start; index+1 < len(markdown); index++ {
		if markdown[index] == ']' && markdown[index+1] == '(' && !escapedAt(markdown, index) {
			return index
		}
	}
	return -1
}

func findImageTargetEnd(markdown string, start int) (int, bool) {
	depth := 1
	var quote byte
	for index := start; index < len(markdown); index++ {
		character := markdown[index]
		if character == '\\' {
			index++
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if (character == '"' || character == '\'') && index > start &&
			isMarkdownSpace(markdown[index-1]) {
			quote = character
			continue
		}
		switch character {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}

func splitImageDestination(raw string) (start, end int, ref string, ok bool) {
	start = 0
	for start < len(raw) && isMarkdownSpace(raw[start]) {
		start++
	}
	if start == len(raw) {
		return 0, 0, "", false
	}
	if raw[start] == '<' {
		closeIndex := strings.IndexByte(raw[start+1:], '>')
		if closeIndex < 0 {
			return 0, 0, "", false
		}
		end = start + closeIndex + 2
		return start, end, raw[start+1 : end-1], true
	}
	end = start
	for end < len(raw) && !isMarkdownSpace(raw[end]) {
		end++
	}
	if end == start {
		return 0, 0, "", false
	}
	return start, end, raw[start:end], true
}

func escapedAt(value string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func isMarkdownSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}
