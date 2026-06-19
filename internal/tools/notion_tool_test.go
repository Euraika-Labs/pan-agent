package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Phase 13 WS#13.D — Notion tool tests. Hermetic via httptest.

func installNotionFake(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	prev := notionAPIBaseFn
	notionAPIBaseFn = func() string { return srv.URL }
	t.Cleanup(func() { notionAPIBaseFn = prev })
	return srv
}

// ---------------------------------------------------------------------------
// Auth + dispatch
// ---------------------------------------------------------------------------

func TestNotion_NoAPIKey(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "")
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"x"}`))
	if !strings.Contains(out.Error, "NOTION_API_KEY") {
		t.Errorf("expected key error, got %+v", out)
	}
}

func TestNotion_InvalidJSON(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{not-json`))
	if !strings.Contains(out.Error, "invalid parameters") {
		t.Errorf("expected parse error, got %+v", out)
	}
}

func TestNotion_UnknownAction(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"create_page"}`))
	if !strings.Contains(out.Error, "unknown action") {
		t.Errorf("expected unknown-action error, got %+v", out)
	}
}

func TestNotion_EmptyAction(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if !strings.Contains(out.Error, "action required") {
		t.Errorf("expected action-required error, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func TestNotion_Search_HappyPath(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret_fake" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("Notion-Version"); got != notionAPIVersion {
			t.Errorf("Notion-Version = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"page_size":10`) {
			t.Errorf("body should include page_size:10, got %s", body)
		}
		fmt.Fprint(w, `{
			"results": [
				{
					"object": "page",
					"id": "abcdef0123456789abcdef0123456789",
					"url": "https://notion.so/page-1",
					"properties": {
						"Name": {"title": [{"plain_text": "First Page"}]}
					}
				},
				{
					"object": "database",
					"id": "11112222333344445555666677778888",
					"url": "https://notion.so/db-1",
					"title": [{"plain_text": "My Database"}]
				}
			],
			"has_more": false
		}`)
	})

	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"hello"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}

	var resp struct {
		Results []struct {
			Object, ID, Title, URL string
		}
	}
	if err := json.Unmarshal([]byte(out.Output), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].Title != "First Page" {
		t.Errorf("Title[0] = %q, want First Page", resp.Results[0].Title)
	}
	if resp.Results[1].Title != "My Database" {
		t.Errorf("Title[1] = %q, want My Database", resp.Results[1].Title)
	}
}

func TestNotion_Search_LimitClamp(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	var captured int
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			PageSize int `json:"page_size"`
		}
		_ = json.Unmarshal(body, &req)
		captured = req.PageSize
		fmt.Fprint(w, `{"results":[]}`)
	})

	tool := NotionTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"x","limit":500}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != 100 {
		t.Errorf("limit clamp: got %d, want 100", captured)
	}
}

func TestNotion_Search_FilterPage(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	var capturedFilter string
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Filter map[string]string `json:"filter"`
		}
		_ = json.Unmarshal(body, &req)
		capturedFilter = req.Filter["value"]
		fmt.Fprint(w, `{"results":[]}`)
	})
	tool := NotionTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"x","filter":"page"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedFilter != "page" {
		t.Errorf("filter = %q, want page", capturedFilter)
	}
}

func TestNotion_Search_HTTPError(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"object":"error","status":401,"code":"unauthorized","message":"API token is invalid"}`,
			http.StatusUnauthorized)
	})
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"x"}`))
	if !strings.Contains(out.Error, "401") {
		t.Errorf("expected 401, got %+v", out)
	}
}

// ---------------------------------------------------------------------------
// get_page
// ---------------------------------------------------------------------------

func TestNotion_GetPage_HappyPath(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pages/abcdef0123456789abcdef0123456789" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"object":"page","id":"abcdef0123456789abcdef0123456789","properties":{}}`)
	})
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_page","id":"abcdef0123456789abcdef0123456789"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.Contains(out.Output, "abcdef0123456789") {
		t.Errorf("output should contain page id: %s", out.Output)
	}
	if !strings.Contains(out.Output, "notion.so") {
		t.Errorf("output should include notion.so URL: %s", out.Output)
	}
}

func TestNotion_GetPage_BadID(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	tool := NotionTool{}
	for _, bad := range []string{
		"",
		"too-short",
		"not-32-hex-but-padded-out-to-look-likeXXXXXX",
		"contains_underscore_chars_____________",
	} {
		out, _ := tool.Execute(context.Background(),
			json.RawMessage(fmt.Sprintf(`{"action":"get_page","id":%q}`, bad)))
		if out.Error == "" {
			t.Errorf("id %q: expected error", bad)
		}
	}
}

func TestNotion_GetPage_UUIDForm(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	var capturedPath string
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		fmt.Fprint(w, `{"object":"page"}`)
	})
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_page","id":"abcdef01-2345-6789-abcd-ef0123456789"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
	if !strings.HasPrefix(capturedPath, "/pages/abcdef01-") {
		t.Errorf("path = %q", capturedPath)
	}
}

// ---------------------------------------------------------------------------
// get_block
// ---------------------------------------------------------------------------

func TestNotion_GetBlock_HappyPath(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/blocks/") {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.HasSuffix(r.URL.Path, "/children") {
			t.Errorf("path missing /children: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("page_size"); got != "100" {
			t.Errorf("page_size = %q, want 100", got)
		}
		fmt.Fprint(w, `{"results":[],"has_more":false}`)
	})
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_block","id":"11112222333344445555666677778888"}`))
	if out.Error != "" {
		t.Fatalf("Error: %s", out.Error)
	}
}

func TestNotion_GetBlock_LimitClamp(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	var captured string
	installNotionFake(t, func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query().Get("page_size")
		fmt.Fprint(w, `{"results":[]}`)
	})
	tool := NotionTool{}
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_block","id":"abcdef0123456789abcdef0123456789","limit":500}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured != "100" {
		t.Errorf("limit clamp: got %q, want 100", captured)
	}
}

func TestNotion_GetBlock_BadID(t *testing.T) {
	t.Setenv("NOTION_API_KEY", "secret_fake")
	tool := NotionTool{}
	out, _ := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"get_block","id":"not-a-real-id"}`))
	if out.Error == "" {
		t.Error("expected bad-id error")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestNotion_RegisteredInRegistry(t *testing.T) {
	tool, ok := Get("notion")
	if !ok {
		t.Fatal("notion tool not registered")
	}
	if tool.Name() != "notion" {
		t.Errorf("Name = %q", tool.Name())
	}
}

func TestNotion_IsNotionID(t *testing.T) {
	cases := map[string]bool{
		"abcdef0123456789abcdef0123456789":      true,
		"abcdef01-2345-6789-abcd-ef0123456789":  true,
		"ABCDEF0123456789ABCDEF0123456789":      true,
		"":                                      false,
		"too_short":                             false,
		"abcdef0123456789abcdef012345678":       false, // 31 chars
		"abcdef0123456789abcdef01234567890":     false, // 33 chars
		"non-hex-but-padded-to-32-charszzzzzz!": false,
		"abcdef01-2345-6789-abcd-ef012345678":   false,
		"abcdef01_2345_6789_abcd_ef0123456789":  false, // underscores not allowed
	}
	for in, want := range cases {
		if got := isNotionID(in); got != want {
			t.Errorf("isNotionID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNotion_TitleExtraction(t *testing.T) {
	cases := []struct {
		name     string
		topRaw   string
		propsRaw string
		want     string
	}{
		{
			name:     "top-level title",
			topRaw:   `[{"plain_text":"Top Title"}]`,
			propsRaw: `{}`,
			want:     "Top Title",
		},
		{
			name:     "name property",
			topRaw:   `[]`,
			propsRaw: `{"Name":{"title":[{"plain_text":"Name Field"}]}}`,
			want:     "Name Field",
		},
		{
			name:     "title property fallback",
			topRaw:   `[]`,
			propsRaw: `{"Other":{"checkbox":true},"title":{"title":[{"plain_text":"Title Field"}]}}`,
			want:     "Title Field",
		},
		{
			name:     "no title anywhere",
			topRaw:   `[]`,
			propsRaw: `{"Description":{"rich_text":[]}}`,
			want:     "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var top []struct {
				PlainText string `json:"plain_text"`
			}
			_ = json.Unmarshal([]byte(c.topRaw), &top)
			var props map[string]any
			_ = json.Unmarshal([]byte(c.propsRaw), &props)
			got := extractNotionTitle(top, props)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNotion_TruncateBody(t *testing.T) {
	if got := truncateNotionBody("short"); got != "short" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("x", 1000)
	got := truncateNotionBody(long)
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("long not truncated: %s", got[len(got)-30:])
	}
}
