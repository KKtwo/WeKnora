package docparser

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestResolveDataURIImagesPreservesGitSourcePath(t *testing.T) {
	png := createTestPNG(200, 150)
	sourcePath := "img/chatbi/architecture.png"
	sourceMarker := base64.RawURLEncoding.EncodeToString([]byte(sourcePath))
	dataURI := "data:image/png;weknora-source=" + sourceMarker + ";base64," +
		base64.StdEncoding.EncodeToString(png)
	svc := &captureSaveBytes{}

	out, images, err := NewImageResolver().ResolveDataURIImages(
		context.Background(),
		"![产品架构]("+dataURI+` "架构图")`,
		svc,
		1,
	)

	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d images, want 1", len(images))
	}
	if images[0].OriginalRef != sourcePath {
		t.Fatalf("original ref = %q, want %q", images[0].OriginalRef, sourcePath)
	}
	if len(svc.saved) != 1 || !bytes.Equal(svc.saved[0], png) {
		t.Fatal("SaveBytes payload mismatch")
	}
	if out == "" || out == "![产品架构]("+dataURI+` "架构图")` {
		t.Fatalf("data URI was not replaced: %q", out)
	}
}

func TestResolveDataURIImagesRejectsForgedGitSourcePaths(t *testing.T) {
	png := createTestPNG(200, 150)
	for _, sourcePath := range []string{
		"https://example.test/image.png",
		"/absolute/image.png",
		"../../escape.png",
		"images/\x00control.png",
		strings.Repeat("a", 2049) + ".png",
	} {
		t.Run(sourcePath[:min(32, len(sourcePath))], func(t *testing.T) {
			sourceMarker := base64.RawURLEncoding.EncodeToString([]byte(sourcePath))
			dataURI := "data:image/png;weknora-source=" + sourceMarker + ";base64," +
				base64.StdEncoding.EncodeToString(png)
			svc := &captureSaveBytes{}

			_, images, err := NewImageResolver().ResolveDataURIImages(
				context.Background(), "![image]("+dataURI+")", svc, 1,
			)

			if err != nil {
				t.Fatal(err)
			}
			if len(images) != 1 {
				t.Fatalf("got %d images, want 1", len(images))
			}
			if images[0].OriginalRef != "embedded-image-data-uri" {
				t.Fatalf("unsafe original ref persisted: %q", images[0].OriginalRef)
			}
			if strings.Contains(images[0].OriginalRef, "data:image") {
				t.Fatal("base64 data URI leaked into original ref")
			}
		})
	}
}

func TestResolveHTMLDataURIImagesRejectsForgedGitSourcePath(t *testing.T) {
	png := createTestPNG(200, 150)
	sourceMarker := base64.RawURLEncoding.EncodeToString([]byte("../escape.png"))
	dataURI := "data:image/png;weknora-source=" + sourceMarker + ";base64," +
		base64.StdEncoding.EncodeToString(png)
	svc := &captureSaveBytes{}

	_, images, err := NewImageResolver().ResolveHTMLDataURIImages(
		context.Background(), `<img src="`+dataURI+`">`, svc, 1,
	)

	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].OriginalRef != "html-img-data-uri" {
		t.Fatalf("unsafe HTML original ref persisted: %#v", images)
	}
}
